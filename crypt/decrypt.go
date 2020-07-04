package crypt

import (
	"fmt"
	"github.com/minio/sio"
	"github.com/spf13/viper"
	"golang.org/x/crypto/scrypt"
	"io"
	"os"
)

func DecryptFile(output string) error {
	out, err := os.Create(output)
		if err != nil {
			return err
		}

	defer out.Close()

	in, err := os.Open("test.txt")
		if err != nil {
			return err
		}

	defer in.Close()

	key := deriveDecryptionKey(in)

	cfg := sio.Config{
		MinVersion: sio.Version20,
		Key: key,
		CipherSuites: []byte{sio.AES_256_GCM},
	}

	if _, err := sio.Decrypt(out, in, cfg); err != nil {
		return err
	}

	return nil
}

func deriveDecryptionKey(in *os.File) []byte {
	password := []byte(viper.GetString("encryption"))
	salt := make([]byte, 32)

	if _, err := io.ReadFull(in, salt); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// https://github.com/minio/sio/blob/master/cmd/ncrypt/main.go#L251
	key, err := scrypt.Key(password, salt, 32768, 16, 1, 32)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return key
}
