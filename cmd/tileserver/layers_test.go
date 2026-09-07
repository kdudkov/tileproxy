package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdudkov/tileproxy/pkg/model"
)

type trackedSource struct {
	model.Source
	key    string
	file   bool
	closed atomic.Int32
}

func (s *trackedSource) GetKey() string { return s.key }
func (s *trackedSource) IsFile() bool   { return s.file }
func (s *trackedSource) Close() error   { s.closed.Add(1); return nil }

func TestLayerReplacementWaitsForReader(t *testing.T) {
	layers := NewLayers()
	defer layers.Clear()
	old := &trackedSource{key: "file", file: true}
	proxy := &trackedSource{key: "proxy"}
	next := &trackedSource{key: "file", file: true}
	layers.Add(old)
	layers.Add(proxy)
	current, release := layers.Acquire("file")
	if current != old {
		release()
		t.Fatal("wrong source")
	}
	started, done := make(chan struct{}), make(chan struct{})
	go func() { close(started); layers.ReplaceFiles([]model.Source{next}); close(done) }()
	<-started
	select {
	case <-done:
		release()
		t.Fatal("replacement did not wait for active reader")
	case <-time.After(30 * time.Millisecond):
	}
	if old.closed.Load() != 0 {
		release()
		t.Fatal("closed active reader")
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("replacement stuck")
	}
	if old.closed.Load() != 1 || proxy.closed.Load() != 0 {
		t.Fatal("incorrect sources closed")
	}
	current, release = layers.Acquire("file")
	release()
	if current != next {
		t.Fatal("new source not published")
	}
	layers.Remove("file")
	if next.closed.Load() != 1 {
		t.Fatal("removed source not closed")
	}
	layers.Clear()
	if proxy.closed.Load() != 1 {
		t.Fatal("clear did not close remaining source")
	}
}

func TestAddClosesReplacedLayer(t *testing.T) {
	layers := NewLayers()
	defer layers.Clear()
	old := &trackedSource{key: "same", file: true}
	next := &trackedSource{key: "same", file: true}
	layers.Add(old)
	layers.Add(old)
	if old.closed.Load() != 0 {
		t.Fatal("readding same instance closed it")
	}
	layers.Add(next)
	if old.closed.Load() != 1 {
		t.Fatal("replaced instance not closed")
	}
}

func TestFileReloadClosesOldSQLiteLayer(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "test.mbtiles")
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
	app := NewApp("")
	app.filesDir = dir
	defer app.close()
	if err := app.addFileSources(); err != nil {
		t.Fatal(err)
	}
	old, release := app.layers.Acquire("test.mbtiles")
	release()
	if old == nil {
		t.Fatal("layer not loaded")
	}
	if err := app.addFileSources(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := old.GetTile(context.Background(), 0, 0, 0); err == nil {
		t.Fatal("old database still open")
	}
	current, release := app.layers.Acquire("test.mbtiles")
	_, data, err := current.GetTile(context.Background(), 0, 0, 0)
	release()
	if err != nil || len(data) != 1 {
		t.Fatalf("replacement unreadable: %v %v", data, err)
	}
	app.close()
	if _, _, err := current.GetTile(context.Background(), 0, 0, 0); err == nil {
		t.Fatal("database open after app close")
	}
}
