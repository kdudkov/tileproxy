package model

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

type Source interface {
	GetTile(ctx context.Context, z, x, y int) (string, []byte, error)
	GetMinZoom() int
	GetMaxZoom() int
	GetKey() string
	GetName() string
	IsTms() bool
	IsFile() bool
}

var _ Source = &Layer{}

type Layer struct {
	minZoom     int
	maxZoom     int
	key         string
	name        string
	contentType string
	db          *sql.DB
	tms         bool
	meta        map[string]string
	modTime     time.Time
}

func NewLayer(key, path string) (*Layer, error) {
	fileInfo, err := os.Stat(path)

	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)

	if err != nil {
		return nil, err
	}

	l := &Layer{
		db:      db,
		key:     key,
		name:    key,
		tms:     true,
		modTime: fileInfo.ModTime(),
	}

	if  err := l.getMetadata(); err != nil {
		return nil, err
	}

	l.minZoom, l.maxZoom, err = l.getMinMaxZoom()
	if err != nil {
		return nil, err
	}

	if v, ok := l.meta["minzoom"]; ok {
		if vv, err := strconv.Atoi(v); err == nil {
			l.minZoom = vv
		}
	}

	if v, ok := l.meta["maxzoom"]; ok {
		if vv, err := strconv.Atoi(v); err == nil {
			l.maxZoom = vv
		}
	}

	if v, ok := l.meta["scheme"]; ok {
		if v != "tms" {
			l.tms = false
		}
	}

	if v, ok := l.meta["name"]; ok {
		l.name = v
	}

	l.contentType = "image/png"

	if v, ok := l.meta["format"]; ok {
		switch v {
		case "png":
			l.contentType = "image/png"
		case "jpg", "jpeg":
			l.contentType = "image/jpeg"
		case "webp":
			l.contentType = "image/webp"
		default:
			return nil, fmt.Errorf("invalid format - %s", v)
		}
	}

	return l, nil
}

func (l *Layer) String() string {
	return fmt.Sprintf("%s %d:%d %v %v %+v", l.name, l.minZoom, l.maxZoom, l.tms, l.modTime, l.meta)
}

func (l *Layer) GetKey() string {
	return l.key
}

func (l *Layer) GetName() string {
	return l.name
}

func (l *Layer) GetMinZoom() int {
	return l.minZoom
}

func (l *Layer) GetMaxZoom() int {
	return l.maxZoom
}

func (l *Layer) IsTms() bool {
	return l.tms
}

func (l *Layer) IsFile() bool {
	return true
}

func (l *Layer) GetModTime() time.Time {
	return l.modTime
}

func (l *Layer) getMetadata() error {
	row, err := l.db.Query("SELECT name,value FROM metadata ORDER BY name")
	if err != nil {
		return err
	}

	l.meta = make(map[string]string)
	
	defer row.Close()
	for row.Next() { // Iterate and fetch the records from result cursor
		var name string
		var value string
		if err = row.Scan(&name, &value); err != nil {
			return err
		}
		l.meta[name] = value
	}

	return nil
}

func (l *Layer) getMinMaxZoom() (int, int, error) {
	row, err := l.db.Query("SELECT min(zoom_level), max(zoom_level) FROM tiles")
	if err != nil {
		return 0, 0, err
	}

	var zmin, zmax int

	defer row.Close()
	if row.Next() {
		if err = row.Scan(&zmin, &zmax); err != nil {
			return 0, 0, err
		}
	}

	return zmin, zmax, nil
}

func (l *Layer) GetTile(ctx context.Context, zoom, x, y int) (string, []byte, error) {
	if l.tms {
		y = 1<<zoom - y - 1
	}

	row, err := l.db.Query("SELECT tile_data FROM tiles WHERE zoom_level=? and tile_column=? and tile_row=?", zoom, x, y)
	if err != nil {
		return "", nil, err
	}

	defer row.Close()

	if row.Next() {
		var data []byte
		if err = row.Scan(&data); err != nil {
			return "", nil, err
		}

		return l.contentType, data, nil
	}

	return "", nil, nil
}
