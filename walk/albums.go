package walk

import "fmt"
import "io/ioutil"
import "os"
import "path"
import "regexp"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/util"

func walkAndProcessArtistDir(verbose bool, dry bool, dir string, conf config.Config) [2]int {
	albums, err := ioutil.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	dirCount := 0
	fileCount := 0

	for _, album := range albums {
		if album.IsDir() {
			dirCount += 1
			util.Printf(fmt.Sprintf("Found album: %s\n", album.Name()), color.Cyan)
			albumdir := handleAlbumDir(verbose, dry, dir, album.Name(), conf)
			if albumdir != "" {
				counts := walkAndProcessAlbumDir(verbose, dry, path.Join(dir, albumdir), conf)
				dirCount += counts[0]
				fileCount += counts[1]
			}
		}
	}

	return [2]int{dirCount, fileCount}
}

func handleAlbumDir(verbose bool, dry bool, workdir string, dir string, conf config.Config) string {
	if m, _ := regexp.MatchString("^\\[\\d{4}\\] .*$", dir); m {
		year := dir[1:5]
		title := dir[7:len(dir)]

		sanitized := util.Sanitize(title, conf.AlbumMaxlen)

		if title != sanitized {
			newdir := fmt.Sprintf("[%s] %s", year, sanitized)
			if verbose {
				util.Printf(fmt.Sprintf("Rename %s %s\n", dir, newdir), color.Yellow)
			}

			if !dry {
				err := os.Rename(path.Join(workdir, dir), path.Join(workdir, newdir))

				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}

				return newdir
			}
		}

		return dir
	}

	return ""
}
