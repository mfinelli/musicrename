package util

import (
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
