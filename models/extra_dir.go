package models

import "fmt"
import "os"
import "path"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/util"

type ExtraDir struct {
	Album    *Album
	RealPath string
	Name     string
}

func (ed *ExtraDir) String() string {
	return ed.Name
}

func (ed *ExtraDir) FullPath() string {
	return path.Join(ed.Album.FullPath(), ed.RealPath)
}

func ParseExtraDir(dir string) (ExtraDir, error) {
	return ExtraDir{
		RealPath: dir,
		Name:     dir,
	}, nil
}

func (ed *ExtraDir) Sanitize(dry bool, conf config.Config) error {
	sanitized := util.Sanitize(ed.Name, conf.ExtraDirMaxlen)

	if sanitized != ed.Name {
		util.Printf(fmt.Sprintf("Rename %s to %s\n", ed.Name, sanitized), color.Yellow)
		ed.Name = sanitized

		if !dry {
			err := os.Rename(ed.FullPath(), path.Join(ed.Album.FullPath(), sanitized))

			if err != nil {
				return err
			}

			ed.RealPath = sanitized
		}
	}

	return nil
}
