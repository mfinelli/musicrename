package util

import (
	"github.com/spf13/viper"
)

func VerifyConfig() bool {
	keys := []string{
		"accesskey",
		"secretkey",
		"encryption",
		"purchases.bucket",
		"purchases.region",
	}

	for _, key := range keys {
		if viper.GetString(key) == "" {
			return false
		}
	}

	return true
}
