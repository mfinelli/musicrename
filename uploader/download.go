package uploader

import (
	"context"
	"errors"
	"github.com/kurin/blazer/b2"
	"github.com/mfinelli/musicrename/util"
	"github.com/spf13/viper"
	"io"
	"os"
)

func Download(bucketName, key, filename string) error {
	ctx := context.Background()

	b2c, err := b2.NewClient(ctx, viper.GetString("accesskey"), viper.GetString("secretkey"))
if err != nil {
	return err
}

bucket, err := b2c.Bucket(ctx, bucketName)
	if err != nil {
		return err
	}

	obj := bucket.Object(key)
	attrs, err := obj.Attrs(ctx)

	if err != nil {
		return err
	}

	size := attrs.Size
	var sha1 string

	if size > 1e8 {
		sha1 = attrs.Info["large_file_sha1"]
	} else {
		sha1 = attrs.SHA1
	}

	r := obj.NewReader(ctx)
	defer r.Close()

	fp, err := openFile(filename)

	if err != nil {
		return err
	}

	if _, err := io.Copy(fp, r); err != nil {
		fp.Close()
		return err
	}

	err = fp.Close()

	if err != nil {
		return err
	}

	actualSha1, err := util.FileSha1(filename)
	if err != nil {
		return err
	}

	if sha1 != actualSha1 {
		return errors.New("sha1 mismatch")
	}

	return nil
}

func openFile(filename string) (*os.File, error) {
	info, err := os.Stat(filename)
    if err != nil {
        return nil, err
    }

    mode := info.Mode()

    return os.OpenFile(filename, os.O_RDWR, mode)
}
