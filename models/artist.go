package models

import "path"

type Artist struct {
	RootDir  string
	RealPath string
	Name     string
	Albums   []Album
}

func (a *Artist) String() string {
	return a.Name
}

func (a *Artist) FullPath() string {
	return path.Join(a.RootDir, a.RealPath)
}

func (a *Artist) AddAlbum(album Album) []Album {
	album.Artist = a
	a.Albums = append(a.Albums, album)
	return a.Albums
}

func ParseArtist(rootDir, dir string) Artist {
	return Artist{
		RootDir:  rootDir,
		RealPath: dir,
		Name:     dir,
	}
}
