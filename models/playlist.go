package models

import "fmt"
import "path"

type Playlist struct {
	Album    *Album
	RealPath string
	Name     string
	Format   string
}

func (p *Playlist) String() string {
	return fmt.Sprintf("%s.%s", p.Name, p.Format)
}

func (p *Playlist) FullPath() string {
	return path.Join(p.Album.FullPath(), p.RealPath)
}

func ParsePlaylist(item string) (Playlist, error) {
	ext := path.Ext(item)
	name := item[0 : len(item)-len(ext)]

	return Playlist{
		RealPath: item,
		Name:     name,
		Format:   ext[1:len(ext)],
	}, nil
}
