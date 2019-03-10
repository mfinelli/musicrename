package walk

import "fmt"
import "io/ioutil"
import "os"
import "path"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/util"

func WalkAndProcessDirectory(verbose bool, dry bool, dir string) [2]int {
	artists, err := ioutil.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	dirCount := 0
	fileCount := 0

	for _, artist := range artists {
		if artist.IsDir() {
			dirCount += 1
			util.Printf(fmt.Sprintf("Found artist: %s\n", artist.Name()), color.Cyan)
			artistdir := handleArtistDir(verbose, dry, dir, artist.Name())
			counts := walkAndProcessArtistDir(verbose, dry, path.Join(dir, artistdir))
			dirCount += counts[0]
			fileCount += counts[1]
		}
	}

	return [2]int{dirCount, fileCount}
}

func handleArtistDir(verbose bool, dry bool, workdir string, dir string) string {
	sanitized := util.Sanitize(dir)

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
