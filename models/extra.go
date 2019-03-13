package models

import "fmt"

type Extra struct {
	ExtraDir *ExtraDir
	RealPath string
	Name     string
	Format   string
}

func (e *Extra) String() string {
	return fmt.Sprintf("%s.%s", e.Name, e.Format)
}
