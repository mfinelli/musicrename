package config

import "bytes"
import "io/ioutil"

import "github.com/BurntSushi/toml"

const ARTIST_MAXLEN = 32
const ALBUM_MAXLEN = 29
const EXTRA_DIR_MAXLEN = 13
const EXTRA_MAXLEN = 25
const SONG_MAXLEN = 38

type Config struct {
	ArtistMaxlen   int `toml:"artist_maxlen"`
	AlbumMaxlen    int `toml:"album_maxlen"`
	ExtraDirMaxlen int `toml:"extra_dir_maxlen"`
	ExtraMaxlen    int `toml:"extra_maxlen"`
	SongMaxlen     int `toml:"song_maxlen"`
}

func defaults() Config {
	return Config{
		ArtistMaxlen:   ARTIST_MAXLEN,
		AlbumMaxlen:    ALBUM_MAXLEN,
		ExtraDirMaxlen: EXTRA_DIR_MAXLEN,
		ExtraMaxlen:    EXTRA_MAXLEN,
		SongMaxlen:     SONG_MAXLEN,
	}
}

func writeDefaults(confPath string) {
	var b bytes.Buffer
	t := toml.NewEncoder(&b)
	t.Encode(defaults())
	ioutil.WriteFile(confPath, b.Bytes(), 0644)
}
