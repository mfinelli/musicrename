package crypt

import (
	"crypto/rand"
	"fmt"
	"github.com/minio/sio"
	"github.com/spf13/viper"
	"golang.org/x/crypto/scrypt"
	"io"
	"os"
)

func EncryptFile(in, out *os.File) error {
	key := deriveEncryptionKey(out)

	cfg := sio.Config{
		MinVersion: sio.Version20,
		Key: key,
		CipherSuites: []byte{sio.AES_256_GCM},
	}

	if _, err := sio.Encrypt(out, in, cfg); err != nil {
		return err
	}

	return nil
}

func deriveEncryptionKey(out *os.File) []byte {
	password := []byte(viper.GetString("encryption"))
	salt := make([]byte, 32)

	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		fmt.Println("could not generate salt")
		os.Exit(1)
	}

	if _, err := out.Write(salt); err != nil {
		fmt.Println("could not write salt")
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
