package walk

import "fmt"
import "io/ioutil"
import "os"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/models"
import "github.com/mfinelli/musicrename/util"

func WalkAndProcessDirectory(verbose bool, dry bool, dir string, conf config.Config) [2]int {
	artists, err := ioutil.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	dirCount := 0
	fileCount := 0

	for _, item := range artists {
		if item.IsDir() {
			dirCount += 1

			artist := models.ParseArtist(dir, item.Name())

			if verbose {
				util.Printf(fmt.Sprintf("Found artist: %s\n", artist.Name), color.Cyan)
			}

			err := artist.Sanitize(dry, conf)

			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			counts := walkAndProcessArtistDir(verbose, dry, &artist, conf)
			dirCount += counts[0]
			fileCount += counts[1]
		}
	}

	return [2]int{dirCount, fileCount}
}
