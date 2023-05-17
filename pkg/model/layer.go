package model

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync"

	_ "modernc.org/sqlite"
)

type Source interface {
	GetTile(ctx context.Context, z, x, y int) ([]byte, error)
	GetMinZoom() int
	GetMaxZoom() int
	GetKey() string
	GetName() string
	GetContentType() string
	IsTms() bool
	IsFile() bool
}

type Tile struct {
	X int
	Y int
	Z int
}

type Layer struct {
	minZoom     int
	maxZoom     int
	key         string
	name        string
	contentType string
	db          *sql.DB
	tms         bool
	mx          sync.Mutex
}

func NewLayer(key, path string) (*Layer, error) {
	db, err := sql.Open("sqlite", path)

	if err != nil {
		return nil, err
	}

	l := &Layer{
		db:   db,
		key:  key,
		name: key,
		tms:  true,
	}

	meta, err := l.getMetadata()
	if err != nil {
		return nil, err
	}

	l.minZoom, l.maxZoom, err = l.getMinMaxZoom()
	if err != nil {
		return nil, err
	}

	if v, ok := meta["minzoom"]; ok {
		if vv, err := strconv.Atoi(v); err == nil {
			l.minZoom = vv
		}
	}

	if v, ok := meta["maxzoom"]; ok {
		if vv, err := strconv.Atoi(v); err == nil {
			l.maxZoom = vv
		}
	}

	if v, ok := meta["scheme"]; ok {
		if v != "tms" {
			l.tms = false
		}
	}

	if v, ok := meta["name"]; ok {
		l.name = v
	}

	l.contentType = "image/png"

	if v, ok := meta["format"]; ok {
		switch v {
		case "png":
			l.contentType = "image/png"
		case "jpg", "jpeg":
			l.contentType = "image/jpeg"
		}
	}

	return l, nil
}

func (t *Tile) InRect(t1, t2 *Tile) bool {
	x1 := t.X * (1 << (19 - t.Z))
	y1 := t.Y * (1 << (19 - t.Z))

	xmin := t1.X * (1 << (19 - t1.Z))
	xmax := (t2.X + 1) * (1 << (19 - t2.Z))

	ymin := t1.Y * (1 << (19 - t1.Z))
	ymax := (t2.Y + 1) * (1 << (19 - t2.Z))

	return x1 >= xmin && x1 < xmax && y1 >= ymin && y1 < ymax
}

func (l *Layer) String() string {
	return fmt.Sprintf("%s %d:%d %v", l.name, l.minZoom, l.maxZoom, l.tms)
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

func (l *Layer) GetContentType() string {
	return l.contentType
}

func (l *Layer) IsTms() bool {
	return l.tms
}

func (l *Layer) IsFile() bool {
	return true
}

func (l *Layer) getMetadata() (map[string]string, error) {
	l.mx.Lock()
	defer l.mx.Unlock()

	row, err := l.db.Query("SELECT name,value FROM metadata ORDER BY name")
	if err != nil {
		return nil, err
	}

	res := make(map[string]string)
	defer row.Close()
	for row.Next() { // Iterate and fetch the records from result cursor
		var name string
		var value string
		if err = row.Scan(&name, &value); err != nil {
			return nil, err
		}
		res[name] = value
	}

	return res, nil
}

func (l *Layer) getMinMaxZoom() (int, int, error) {
	l.mx.Lock()
	defer l.mx.Unlock()

	row, err := l.db.Query("SELECT zoom_level, min(tile_column), max(tile_column), min(tile_row), max(tile_row) from tiles group by zoom_level order by zoom_level")
	if err != nil {
		return 0, 0, err
	}

	minv := -1
	maxv := -1

	defer row.Close()
	for row.Next() {
		var minx, maxx, miny, maxy, z int
		if err = row.Scan(&z, &minx, &maxx, &miny, &maxy); err != nil {
			return 0, 0, err
		}
		if minv == -1 || z < minv {
			minv = z
		}
		if maxv == -1 || z > maxv {
			maxv = z
		}
	}

	return minv, maxv, nil
}

func (l *Layer) GetTile(ctx context.Context, zoom, x, y int) ([]byte, error) {
	if l.tms {
		y = 1<<zoom - y - 1
	}

	row, err := l.db.Query("SELECT tile_data FROM tiles WHERE zoom_level=? and tile_column=? and tile_row=?", zoom, x, y)
	if err != nil {
		return nil, err
	}

	defer row.Close()

	if row.Next() {
		var data []byte
		if err = row.Scan(&data); err != nil {
			return nil, err
		}

		return data, nil
	}

	return nil, nil
}
