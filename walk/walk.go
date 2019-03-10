package walk

import "fmt"
import "io/ioutil"
import "os"

import "github.com/gookit/color"

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
			fmt.Println(color.Cyan.Sprintf("Found artist: %s", artist.Name()))
		}
	}

	return [2]int{dirCount, fileCount}
}
