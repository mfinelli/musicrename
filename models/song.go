package models

import "errors"
import "fmt"
import "path"
import "regexp"
import "strconv"

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
