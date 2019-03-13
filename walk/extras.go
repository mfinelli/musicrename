package walk

import "errors"
import "fmt"
import "io/ioutil"
import "os"
import "path"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/models"
import "github.com/mfinelli/musicrename/util"

func walkAndProcessExtraDir(verbose bool, dry bool, extradir *models.ExtraDir, conf config.Config) int {
	extras, err := ioutil.ReadDir(extradir.FullPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fileCount := 0

	for _, item := range extras {
		if item.IsDir() {
			fmt.Fprintln(os.Stderr, errors.New(fmt.Sprintf("too many directories: %s\n", path.Join(extradir.FullPath(), item.Name()))))
			os.Exit(1)
		} else {
			fileCount += 1
			extra, err := models.ParseExtra(item.Name())

			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			extradir.AddExtra(&extra)

			if verbose {
				util.Printf(fmt.Sprintf("Found extra: %s\n", extra.String()), color.Cyan)
			}

			err = extra.Sanitize(dry, conf)

			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}

	return fileCount
}
