package models

import "fmt"
import "path"

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
