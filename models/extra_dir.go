package models

import "path"

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
