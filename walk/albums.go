package walk

import "fmt"
import "io/ioutil"
import "os"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/models"
import "github.com/mfinelli/musicrename/util"

func walkAndProcessArtistDir(verbose bool, dry bool, artist *models.Artist, conf config.Config) [2]int {
	albums, err := ioutil.ReadDir(artist.FullPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	dirCount := 0
	fileCount := 0

	for _, item := range albums {
		if item.IsDir() {
			dirCount += 1

			album, err := models.ParseAlbum(item.Name())

			if err == nil {
				artist.AddAlbum(album)

				if verbose {
					util.Printf(fmt.Sprintf("Found album: %s\n", album.String()), color.Cyan)
				}

				al := artist.Albums[len(artist.Albums)-1]
				err := al.Sanitize(dry, conf)

				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}

				counts := walkAndProcessAlbumDir(verbose, dry, al.FullPath(), conf)
				dirCount += counts[0]
				fileCount += counts[1]

			} else {
				util.Printf(fmt.Sprintf("Skipping non-album directory: %s\n", item.Name()), color.Red)
			}
		}
	}

	return [2]int{dirCount, fileCount}
}
