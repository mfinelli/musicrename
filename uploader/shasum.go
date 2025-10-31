package uploader

import (
	"bytes"
	"context"
	"fmt"
	"github.com/kurin/blazer/b2"
	"github.com/spf13/viper"
	"io"
)

func FetchShaSumFile(bucketName, key string) (string, error) {
	ctx := context.Background()

	b2c, err := b2.NewClient(ctx, viper.GetString("accesskey"), viper.GetString("secretkey"))
if err != nil {
	return "", err
}

bucket, err := b2c.Bucket(ctx, bucketName)
	if err != nil {
		return "", err
	}

obj := bucket.Object(key)
fmt.Println(obj.Attrs(ctx))
r := bucket.Object(key).NewReader(ctx)
defer r.Close()

	val := new(bytes.Buffer)

	if _, err := io.Copy(val, r); err != nil {
		return "", err
	}

	return val.String(), nil
}
