package models

import "fmt"
import "path"

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
