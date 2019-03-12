package models

import "fmt"

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
