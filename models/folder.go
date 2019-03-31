package models

import "path"

type Folder struct {
	Album    *Album
	RealPath string
}

func (f *Folder) String() string {
	return "folder.jpg"
}

func (f *Folder) FullPath() string {
	return path.Join(f.Album.FullPath(), f.RealPath)
}

func ParseFolder(item string) (Folder, error) {
	return Folder{
		RealPath: item,
	}, nil
}
