package walk

import "errors"
import "fmt"
import "io/ioutil"
import "os"
import "path"

func walkAndProcessAlbumDir(verbose bool, dry bool, dir string) [2]int {
	songs, err := ioutil.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	dirCount := 0
	fileCount := 0

	for _, song := range songs {
		if song.IsDir() {
			dirCount += 1
		} else {
			fileCount += 1
			handleSong(verbose, dry, dir, song.Name())
		}
	}

	return [2]int{dirCount, fileCount}
}

func handleSong(verbose bool, dry bool, workdir string, song string) string {
	ext := path.Ext(song)
	filename := song[0 : len(song)-len(ext)]
	fmt.Printf("song: %s, ext: %s\n", filename, ext)

	switch ext {
	case ".flac", ".m4a", ".mp3", ".ogg":
	case ".jpg", ".png", ".tiff", ".tif":
	case ".cue":
	case ".log":
	case ".m3u", ".m3u8":
	case ".md5":
	default:
		fmt.Fprintln(os.Stderr, errors.New(fmt.Sprintf("unsupported extension: %s\n", ext)))
		os.Exit(1)
	}

	return song
}
