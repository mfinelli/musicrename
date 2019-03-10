package walk

import "fmt"
import "io/ioutil"
import "os"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/util"

func walkAndProcessArtistDir(verbose bool, dry bool, dir string) [2]int {
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
		}
	}

	return [2]int{dirCount, fileCount}
}
