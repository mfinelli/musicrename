package config

import "errors"
import "fmt"
import "io/ioutil"
import "os"
import "path"
import "runtime"

import "github.com/BurntSushi/toml"

const SETTINGS_FILE = "settings.toml"

// https://github.com/golang/go/commit/ebdc24c3d334132542daa7c57246389e0b259227
func configBasePath() (string, error) {
	var dir string

	switch runtime.GOOS {
	case "windows":
		dir = os.Getenv("AppData")
		if dir == "" {
			return "", errors.New("%AppData% is not defined")
		}

	default:
		dir = os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir = os.Getenv("HOME")
			if dir == "" {
				return "", errors.New("neither $XDG_CONFIG_HOME nor $HOME are defined")
			}
			dir = path.Join(dir, ".config")
		}
	}

	return path.Join(dir, "musicrename"), nil
}

func ReadOrCreateConfigFile() (Config, error) {
	bp, err := configBasePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fullpath := path.Join(bp, SETTINGS_FILE)
	if _, err := os.Stat(fullpath); err == nil {
		settings, err := ioutil.ReadFile(fullpath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		var conf Config
		if _, err := toml.Decode(string(settings), &conf); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		return conf, nil
	} else if os.IsNotExist(err) {
		err = os.MkdirAll(bp, 0755)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		writeDefaults(fullpath)
		return defaults(), nil
	} else {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	return Config{}, errors.New("unable to get configuration")
}
