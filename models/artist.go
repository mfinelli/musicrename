package models

import "fmt"
import "os"
import "path"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/util"

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

func (a *Artist) AddAlbum(album *Album) {
	album.Artist = a
	a.Albums = append(a.Albums, *album)
}

func ParseArtist(rootDir, dir string) Artist {
	return Artist{
		RootDir:  rootDir,
		RealPath: dir,
		Name:     dir,
	}
}

func (a *Artist) Sanitize(dry bool, conf config.Config) error {
	sanitized := util.Sanitize(a.Name, conf.ArtistMaxlen)

	if sanitized != a.Name {
		util.Printf(fmt.Sprintf("Rename %s to %s\n", a.Name, sanitized), color.Yellow)
		a.Name = sanitized

		if !dry {
			err := os.Rename(a.FullPath(), path.Join(a.RootDir, sanitized))

			if err != nil {
				return err
			}

			a.RealPath = sanitized
		}
	}

	return nil
}
