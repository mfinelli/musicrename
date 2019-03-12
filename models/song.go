package models

import "fmt"
import "path"

type Song struct {
	Album  *Album
	Disc   int
	Track  int
	Name   string
	Format string
}

func (s *Song) String() string {
	if s.Disc == 0 {
		return fmt.Sprintf("%02d %s.%s", s.Track, s.Name, s.Format)
	}

	return fmt.Sprintf("%d-%02d %s.%s", s.Disc, s.Track, s.Name, s.Format)
}

func (s *Song) FullPath() string {
	return path.Join(s.Album.FullPath(), s.String())
}
