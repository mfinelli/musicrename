package models

import "errors"
import "fmt"
import "os"
import "path"
import "regexp"
import "strconv"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/util"

type Song struct {
	Album    *Album
	RealPath string
	Disc     int
	Track    int
	Name     string
	Format   string
}

func (s *Song) String() string {
	if s.Disc == 0 {
		return fmt.Sprintf("%02d %s.%s", s.Track, s.Name, s.Format)
	}

	return fmt.Sprintf("%d-%02d %s.%s", s.Disc, s.Track, s.Name, s.Format)
}

func (s *Song) FullPath() string {
	return path.Join(s.Album.FullPath(), s.RealPath)
}

func ParseSong(item string) (Song, error) {
	if m, _ := regexp.MatchString("^\\d-\\d{2} .*[\\.flac|\\.mp3|\\.m4a|\\.ogg]$", item); m {
		disc, err := strconv.Atoi(item[0:1])

		if err != nil {
			return Song{}, err
		}

		track, err := strconv.Atoi(item[2:4])

		if err != nil {
			return Song{}, err
		}

		ext := path.Ext(item)
		name := item[5 : len(item)-len(ext)]

		return Song{
			RealPath: item,
			Disc:     disc,
			Track:    track,
			Name:     name,
			Format:   ext[1:len(ext)],
		}, nil
	} else if m, _ := regexp.MatchString("^\\d{2} .*[\\.flac|\\.mp3|\\.m4a|\\.ogg]$", item); m {
		track, err := strconv.Atoi(item[0:2])

		if err != nil {
			return Song{}, err
		}

		ext := path.Ext(item)
		name := item[3 : len(item)-len(ext)]

		return Song{
			RealPath: item,
			Disc:     0,
			Track:    track,
			Name:     name,
			Format:   ext[1:len(ext)],
		}, nil
	}

	return Song{}, errors.New(fmt.Sprintf("Unable to parse song from: %s", item))
}

func (s *Song) Sanitize(dry bool, conf config.Config) error {
	var original string
	if s.Disc == 0 {
		original = fmt.Sprintf("%02d %s", s.Track, s.Name)
	} else {
		original = fmt.Sprintf("%d-%02d %s", s.Disc, s.Track, s.Name)
	}

	sanitized := util.Sanitize(original, conf.SongMaxlen)

	if sanitized != original {
		newName := fmt.Sprintf("%s.%s", sanitized, s.Format)
		util.Printf(fmt.Sprintf("Rename %s to %s.%s\n", s.String(), newName, s.Format), color.Yellow)

		if s.Disc == 0 {
			s.Name = sanitized[3:len(sanitized)]
		} else {
			s.Name = sanitized[5:len(sanitized)]
		}

		if !dry {
			err := os.Rename(s.FullPath(), path.Join(s.Album.FullPath(), newName))

			if err != nil {
				return err
			}

			s.RealPath = newName
		}
	}

	return nil
}
