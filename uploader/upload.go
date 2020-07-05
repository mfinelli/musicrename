package uploader

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	// "github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"github.com/mfinelli/musicrename/util"

	"github.com/vbauerster/mpb/v5"
	"github.com/vbauerster/mpb/v5/decor"

	"github.com/spf13/viper"

	"github.com/kurin/blazer/b2"

	"io"
)

func Upload2(bucketName, key, filename string) error {
	sha1, err := util.FileSha1(filename)
		if err != nil {
			return err
		}

	ctx := context.Background()

	b2c, err := b2.NewClient(ctx, viper.GetString("accesskey"), viper.GetString("secretkey"))
if err != nil {
	return err
}

	bucket, err := b2c.Bucket(ctx, bucketName)
	if err != nil {
		return err
	}

	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}

	obj := bucket.Object(key)
	// objattr,_ := obj.Attrs(ctx)
	// objattr.Info = map[string]string{
	// 	"Test-Thing": "is a test",
	// }

	var w *b2.Writer
	if fileInfo.Size() > 1e8 {
		w = obj.NewWriter(ctx, b2.WithAttrsOption(&b2.Attrs{
		SHA1: sha1,
		// Info: map[string]string{
		// 	"Test-Thing": "is a test",
		// },
	}))
	} else {
		w = obj.NewWriter(ctx)
	}



	// fmt.Println(fileInfo.Size())
	// fmt.Println(w.ChunkSize)
	// w.WithAttrs
	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return err
	}
	return w.Close()

	// return nil
}

func Upload(bucket, key, filename string) error {
	sha1, err := util.FileSha1(filename)
		if err != nil {
			return err
		}

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

	p := mpb.New()

	reader := &CustomReader{
		fp:   file,
		size: fileInfo.Size(),
		signMap: map[int64]struct{}{},
		bar: p.AddBar(fileInfo.Size(),
	mpb.PrependDecorators(
                // simple name decorator
                decor.Name("uploading..."),
                // decor.DSyncWidth bit enables column width synchronization
                decor.Percentage(decor.WCSyncSpace),
            ),
    ),
	}

	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		// u.PartSize = 5 * 1024 * 1024
		// u.LeavePartsOnError = true
	})

	output, err := uploader.Upload(&s3manager.UploadInput{
		ACL: aws.String("private"),
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   reader,
		Metadata: map[string]*string{
			"X-Bz-Content-Sha1": aws.String(sha1),
			"fileInfo": aws.String(fmt.Sprintf("{\"large_file_sha1\":\"%s\"}", sha1)),
		},
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
