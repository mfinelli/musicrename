package models

import "path"

type Artist struct {
	RootDir string
	Name    string
	Albums  []Album
}

func (a *Artist) String() string {
	return a.Name
}

func (a *Artist) FullPath() string {
	return path.Join(a.RootDir, a.Name)
}

func (a *Artist) AddAlbum(album Album) []Album {
	album.Artist = a
	a.Albums = append(a.Albums, album)
	return a.Albums
}
