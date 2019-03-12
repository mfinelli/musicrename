package models

import "path"

type Artist struct {
	RootDir string
	Name string
}

func (a Artist) String() string {
	return a.Name
}

func (a Artist) FullPath() string {
	return path.Join(a.RootDir, a.Name)
}
