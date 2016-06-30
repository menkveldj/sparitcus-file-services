package services

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/nu7hatch/gouuid"
	"log"
	"github.com/menkveldj/nafue-api/config"
	"github.com/menkveldj/nafue-api/models"
	"github.com/menkveldj/nafue-api/repositories"
	"time"
)

type IFileService interface {
	GetFile(*models.FileHeader) error
	AddFile(*models.FileHeader) error
}

type FileService struct {
	fileRepository           repositories.IFileRepository
	basicAnalyticsRepository repositories.IBasicAnalyticsRepository
}

func NewFileService(fileRepository repositories.IFileRepository, basicAnalyticsRepository repositories.IBasicAnalyticsRepository) *FileService {
	return &FileService{fileRepository, basicAnalyticsRepository}
}

func (self *FileService) GetFile(fileDisplay *models.FileHeader) error {

	// make model from display
	file := models.FileHeader{
		ShortUrl: fileDisplay.ShortUrl,
	}

	// get file from db
	err := self.fileRepository.GetFile(&file)
	if err != nil {
		return err
	}

	// now that we have file delete it from db
	self.fileRepository.DeleteFile(&file)

	// verify that file isn't to old
	elapsed := int(time.Now().Sub(file.Created).Seconds())
	if elapsed > file.TTL {
		// to old delete file
		fmt.Println("file to old, delete from s3!")
		_, err := GetS3Service().DeleteObject(&s3.DeleteObjectInput{
			Bucket: aws.String(config.S3Bucket),
			Key:    aws.String(config.S3Key + "/" + file.S3Path),
		})
		if err != nil {
			fmt.Println("---ERROR---", err.Error())
		}
		return errors.New("File has expired")
	}

	// create get request
	req, _ := GetS3Service().GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(config.S3Bucket),
		Key:    aws.String(config.S3Key + "/" + file.S3Path),
	})

	url, err := req.Presign(15 * time.Minute)
	if err != nil {
		log.Println("--ERROR---", err.Error())
		return err
	}

	// add needed data to display
	fileDisplay.DownloadUrl = url
	fileDisplay.Salt = file.Salt
	fileDisplay.Hmac = file.Hmac

	return nil
}

func (self *FileService) AddFile(fileDisplay *models.FileHeader) error {

	// generate random uuid
	s3u, err := uuid.NewV4()
	if err != nil {
		return err
	}
	shortUrl, err := uuid.NewV4()
	if err != nil {
		return err
	}

	// create put request on s3
	req, _ := GetS3Service().PutObjectRequest(&s3.PutObjectInput{
		Bucket:      aws.String(config.S3Bucket),
		Key:         aws.String(config.S3Key + "/" + s3u.String()),
		ContentType: aws.String("text/plain;charset=UTF-8"),
	})
	//checksum := base64.StdEncoding.EncodeToString(fileDisplay.MD5Checksum)
	//req.HTTPRequest.Header.Set("Content-MD5", checksum)
	//fmt.Println("Checksum: ", checksum)
	url, err := req.Presign(15 * time.Minute)
	if err != nil {
		log.Println("--ERROR---", err.Error())
		return err
	}

	// create domain model from display
	file := models.FileHeader{
		S3Path:    s3u.String(),
		ShortUrl:  shortUrl.String(),
		TTL:       (1 * 60 * 60 * 24), // 24h in seconds
		Salt:      fileDisplay.Salt,
		Hmac:      fileDisplay.Hmac,
		UploadUrl: url,
	}

	// add upload url to display
	fileDisplay.UploadUrl = file.UploadUrl
	fileDisplay.ShortUrl = file.ShortUrl

	// add file to db
	err = self.fileRepository.AddFile(&file)
	if err != nil {
		return err
	}

	self.basicAnalyticsRepository.IncrementFileCount()

	return nil
}
