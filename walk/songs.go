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

	for _, item := range songs {
		if item.IsDir() {
			dirCount += 1

			extradir, err := models.ParseExtraDir(item.Name())

			if err == nil {
				album.AddExtraDir(&extradir)

				if verbose {
					util.Printf(fmt.Sprintf("Found extra dir: %s\n", extradir.String()), color.Cyan)
				}

				err := extradir.Sanitize(dry, conf)

				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}

				fileCount += walkAndProcessExtraDir(verbose, dry, &extradir, conf)
			} else {
				util.Printf(fmt.Sprintf("Skipping non-album directory: %s\n", item.Name()), color.Red)
			}
		} else {
			fileCount += 1
			handleSong(verbose, dry, album, item.Name(), conf)
		}
	}

	return [2]int{dirCount, fileCount}
}

func handleSong(verbose bool, dry bool, album *models.Album, song string, conf config.Config) {
	ext := path.Ext(song)

	switch ext {
	case ".flac", ".m4a", ".mp3", ".ogg":
		song, err := models.ParseSong(song)

		if err == nil {
			album.AddSong(&song)

			if verbose {
				util.Printf(fmt.Sprintf("Found song: %s\n", song.String()), color.Cyan)
			}

			err := song.Sanitize(dry, conf)

			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

	case ".jpg", ".png", ".tiff", ".tif":
		if song == "folder.jpg" {
			break
		}
	case ".cue":
		cue, err := models.ParseCue(song)

		if err == nil {
			album.AddCue(&cue)

			if verbose {
				util.Printf(fmt.Sprintf("Found cue: %s\n", cue.String()), color.Cyan)
			}
		} else {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case ".log":
	case ".m3u", ".m3u8":
		playlist, err := models.ParsePlaylist(song)

		if err == nil {
			album.AddPlaylist(&playlist)

			if verbose {
				util.Printf(fmt.Sprintf("Found playlist: %s\n", playlist.String()), color.Cyan)
			}
		} else {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case ".md5":
	default:
		fmt.Fprintln(os.Stderr, errors.New(fmt.Sprintf("unsupported extension: %s\n", ext)))
		os.Exit(1)
	}
}
