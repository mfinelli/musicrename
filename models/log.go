package models

import "fmt"
import "path"

type Log struct {
	Album    *Album
	RealPath string
	Name     string
}

func (l *Log) String() string {
	return fmt.Sprintf("%s.log", l.Name)
}

func (l *Log) FullPath() string {
	return path.Join(l.Album.FullPath(), l.RealPath)
}

func ParseLog(item string) (Log, error) {
	ext := path.Ext(item)
	name := item[0 : len(item)-len(ext)]

	return Log{
		RealPath: item,
		Name:     name,
	}, nil
}
