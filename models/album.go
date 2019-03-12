package models

import "errors"
import "fmt"
import "path"
import "regexp"
import "strconv"

type Album struct {
	Artist *Artist
	Year   int
	Name   string
	Songs  []Song
}

func (a *Album) String() string {
	return fmt.Sprintf("[%d] %s", a.Year, a.Name)
}

func (a *Album) FullPath() string {
	return path.Join(a.Artist.FullPath(), a.String())
}

func ParseAlbum(dir string) (Album, error) {
	if m, _ := regexp.MatchString("^\\[\\d{4}\\] .*$", dir); m {
		yearStr := dir[1:5]
		title := dir[7:len(dir)]
		year, err := strconv.Atoi(yearStr)

		if err != nil {
			return Album{}, err
		}

		return Album{
			Year: year,
			Name: title,
		}, nil
	}

	return Album{}, errors.New(fmt.Sprintf("Unable to parse album from: %s", dir))
}
