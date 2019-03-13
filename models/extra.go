package models

import "fmt"
import "os"
import "path"

import "github.com/gookit/color"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/util"

type Extra struct {
	ExtraDir *ExtraDir
	RealPath string
	Name     string
	Format   string
}

func (e *Extra) String() string {
	return fmt.Sprintf("%s.%s", e.Name, e.Format)
}

func (e *Extra) FullPath() string {
	return path.Join(e.ExtraDir.FullPath(), e.RealPath)
}

func ParseExtra(item string) (Extra, error) {
	ext := path.Ext(item)
	name := item[0 : len(item)-len(ext)]

	return Extra{
		RealPath: item,
		Name:     name,
		Format:   ext[1:len(ext)],
	}, nil
}

func (e *Extra) Sanitize(dry bool, conf config.Config) error {
	maxlen := conf.ExtraDirMaxlen - len(e.ExtraDir.Name)
	sanitized := util.Sanitize(e.Name, conf.ExtraMaxlen+maxlen)

	if sanitized != e.Name {
		newName := fmt.Sprintf("%s.%s", sanitized, e.Format)
		util.Printf(fmt.Sprintf("Rename %s to %s\n", e.String(), newName), color.Yellow)

		if !dry {
			err := os.Rename(e.FullPath(), path.Join(e.ExtraDir.FullPath(), newName))

			if err != nil {
				return err
			}

			e.RealPath = newName
		}
	}

	return nil
}
