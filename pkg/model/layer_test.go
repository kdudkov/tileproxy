package model

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMultiLayerCloseClosesAllDatabases(t *testing.T) {
	var layers []*Layer
	for _, name := range []string{"a", "b"} {
		filename := filepath.Join(t.TempDir(), name+".mbtiles")
		db, err := sql.Open("sqlite", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`CREATE TABLE metadata(name TEXT,value TEXT); CREATE TABLE tiles(zoom_level INTEGER,tile_column INTEGER,tile_row INTEGER,tile_data BLOB); INSERT INTO tiles VALUES(0,0,0,x'01');`)
		if err != nil {
			db.Close()
			t.Fatal(err)
		}
		db.Close()
		layer, err := NewLayer(name, filename)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { layer.Close() })
		layers = append(layers, layer)
	}
	multi := NewMultilayer("all", "all", layers)
	if _, data, err := multi.GetTile(context.Background(), 0, 0, 0); err != nil || len(data) != 1 {
		t.Fatalf("read: %v %v", data, err)
	}
	if err := multi.Close(); err != nil {
		t.Fatal(err)
	}
	for _, layer := range layers {
		if err := layer.db.Ping(); err == nil {
			t.Fatal("database still open")
		}
	}
	if err := multi.Close(); err != nil {
		t.Fatal("repeated close:", err)
	}
}
