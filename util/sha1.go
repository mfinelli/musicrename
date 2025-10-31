package util

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
)

func FileSha1(file string) (string, error) {
	fp, err := os.Open(file)
	if err != nil {
		return "", err
	}

	defer fp.Close()

	hash := sha1.New()

	if _, err := io.Copy(hash, fp); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func StringSha1(str string) (string, error) {
	r := bytes.NewBufferString(str)
	hash := sha1.New()

	if _,err := io.Copy(hash, r); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
