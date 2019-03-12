package walk

import "errors"
import "fmt"
import "io/ioutil"
import "os"
import "path"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/util"

func handleExtraDir(verbose bool, dry bool, workdir string, dir string, conf config.Config) string {
	sanitized := util.Sanitize(dir, conf.ExtraDirMaxlen)

	if sanitized != dir {
		if verbose {
			util.Printf(fmt.Sprintf("Rename %s to %s\n", dir, sanitized), color.Yellow)
		}

		if !dry {
			err := os.Rename(path.Join(workdir, dir), path.Join(workdir, sanitized))

			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			return sanitized
		}
	}

	return dir
}

func walkAndProcessExtraDir(verbose bool, dry bool, dir string, conf config.Config) int {
	extras, err := ioutil.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fileCount := 0

	for _, extra := range extras {
		if extra.IsDir() {
			fmt.Fprintln(os.Stderr, errors.New(fmt.Sprintf("too many directories: %s\n", path.Join(dir, extra.Name()))))
			os.Exit(1)
		} else {
			fileCount += 1
			handleExtra(verbose, dry, dir, extra.Name(), conf)
		}
	}

	return fileCount
}

func handleExtra(verbose bool, dry bool, workdir string, extra string, conf config.Config) string {
	sanitized := util.Sanitize(extra, conf.ExtraMaxlen)

	if sanitized != extra {
		if verbose {
			util.Printf(fmt.Sprintf("Rename %s to %s\n", extra, sanitized), color.Yellow)
		}

		if !dry {
			err := os.Rename(path.Join(workdir, extra), path.Join(workdir, sanitized))

			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			return sanitized
		}
	}

	return extra
}
