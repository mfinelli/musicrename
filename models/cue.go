package models

import "fmt"
import "path"

type Cue struct {
	Album    *Album
	RealPath string
	Name     string
	Format   string
}

func (c *Cue) String() string {
	return fmt.Sprintf("%s.%s", c.Name, c.Format)
}

func (c *Cue) FullPath() string {
	return path.Join(c.Album.FullPath(), c.RealPath)
}
