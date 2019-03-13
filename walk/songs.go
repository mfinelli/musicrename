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

func walkAndProcessAlbumDir(verbose bool, dry bool, album *models.Album, conf config.Config) [2]int {
	songs, err := ioutil.ReadDir(album.FullPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	dirCount := 0
	fileCount := 0

	for _, song := range songs {
		if song.IsDir() {
			dirCount += 1
			extradir := handleExtraDir(verbose, dry, album.FullPath(), song.Name(), conf)
			if extradir != "" {
				fileCount += walkAndProcessExtraDir(verbose, dry, path.Join(album.FullPath(), extradir), conf)
			}
		} else {
			fileCount += 1
			handleSong(verbose, dry, album.FullPath(), song.Name(), conf)
		}
	}

	return [2]int{dirCount, fileCount}
}

func handleSong(verbose bool, dry bool, workdir string, song string, conf config.Config) string {
	ext := path.Ext(song)
	filename := song[0 : len(song)-len(ext)]

	switch ext {
	case ".flac", ".m4a", ".mp3", ".ogg":
		sanitized := util.Sanitize(filename, conf.SongMaxlen)

		if sanitized != filename {
			if verbose {
				util.Printf(fmt.Sprintf("Rename %s to %s%s\n", song, sanitized, ext), color.Yellow)
			}

			if !dry {
				err := os.Rename(path.Join(workdir, song),
					path.Join(workdir, fmt.Sprintf("%s%s", sanitized, ext)))

				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}

				return fmt.Sprintf("%s%s", sanitized, ext)
			}
		}

		return song

	case ".jpg", ".png", ".tiff", ".tif":
		if song == "folder.jpg" {
			break
		}
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
