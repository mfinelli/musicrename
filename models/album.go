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

type Album struct {
	Artist   *Artist
	RealPath string
	Year     int
	Name     string
	Songs    []Song
}

func (a *Album) String() string {
	return fmt.Sprintf("[%d] %s", a.Year, a.Name)
}

func (a *Album) FullPath() string {
	return path.Join(a.Artist.FullPath(), a.RealPath)
}

func ParseAlbum(dir string) (Album, error) {
	if m, _ := regexp.MatchString("^\\[\\d{4}\\] .*$", dir); m {
		title := dir[7:len(dir)]
		year, err := strconv.Atoi(dir[1:5])

		if err != nil {
			return Album{}, err
		}

		return Album{
			RealPath: dir,
			Year:     year,
			Name:     title,
		}, nil
	}

	return Album{}, errors.New(fmt.Sprintf("Unable to parse album from: %s", dir))
}

func (a *Album) Sanitize(dry bool, conf config.Config) error {
	sanitized := util.Sanitize(a.Name, conf.AlbumMaxlen)

	if sanitized != a.Name {
		newName := fmt.Sprintf("[%d] %s", a.Year, sanitized)
		util.Printf(fmt.Sprintf("Rename %s to %s\n", a.String(), newName), color.Yellow)
		a.Name = sanitized

		if !dry {
			err := os.Rename(a.FullPath(), path.Join(a.Artist.FullPath(), newName))

			if err != nil {
				return err
			}

			a.RealPath = newName
		}
	}

	return nil
}
