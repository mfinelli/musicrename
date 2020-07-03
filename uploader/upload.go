package uploader

import (
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	// "github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"github.com/spf13/viper"
)


func Upload(bucket, key, filename string) error {
	s3Config := &aws.Config{
Credentials: credentials.NewStaticCredentials(viper.GetString("accesskey"), viper.GetString("secretkey"), ""),
Endpoint: aws.String(fmt.Sprintf("https://s3.%s.backblazeb2.com", viper.GetString("purchases.region"))),
Region: aws.String(viper.GetString("purchases.region")),
S3ForcePathStyle: aws.Bool(true),
}

// 	sess, err := session.NewSession(&aws.Config{
//     Region:      aws.String(viper.GetString("purchases.region")),
//     Credentials: credentials.NewStaticCredentials(viper.GetString("accesskey"), viper.GetString("secretkey"), ""),
// })
	sess, err := session.NewSession(s3Config)

	if err != nil {
		return err
	}

	// s3Client := s3.New(sess)

	file, err := os.Open(filename)
	if err != nil {
		return err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}

	reader := &CustomReader{
		fp:   file,
		size: fileInfo.Size(),
	}

	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		// u.PartSize = 5 * 1024 * 1024
		// u.LeavePartsOnError = true
	})

	output, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   reader,
	})

// 	_, err = s3Client.PutObject(&s3.PutObjectInput{
// Body: reader,
// Bucket: aws.String(bucket),
// Key: aws.String(key),
// })
// if err != nil {
// fmt.Printf("Failed to upload object %s/%s, %s\n", bucket, key, err.Error())
// return err
// }
// fmt.Printf("Successfully uploaded key %s\n",key)


	if err != nil {
		return err
	}

	fmt.Println(output)

	return nil
}
