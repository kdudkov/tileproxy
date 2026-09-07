package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdudkov/tileproxy/pkg/model"
)

func TestTileCornerRule(t *testing.T) {
	cases := []struct {
		name string
		p    contour
		want bool
	}{
		{"corner inside", contour{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}, true},
		{"corner on boundary", contour{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, true},
		{"contour entirely inside tile", contour{{10, 10}, {20, 10}, {20, 20}, {10, 20}}, false},
		{"edge crossing without corners", contour{{-1, 10}, {91, 10}, {91, 20}, {-1, 20}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.includes(tile{2, 2, 1}); got != c.want {
				t.Fatalf("includes=%v want %v", got, c.want)
			}
		})
	}
	p := contour{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
	plans, err := p.plan(context.Background(), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{0, 4, 4} {
		if plans[i].count != want {
			t.Fatalf("zoom %d count=%d want %d", i, plans[i].count, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.plan(ctx, 1, 2); err == nil {
		t.Fatal("cancelled planning succeeded")
	}
}
func TestReadContour(t *testing.T) {
	for _, data := range []string{
		`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,1],[1,0],[0,0]]]}`,
	} {
		name := filepath.Join(t.TempDir(), "contour.geojson")
		os.WriteFile(name, []byte(data), 0600)
		if _, err := readContour(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []string{
		`{"type":"Polygon","coordinates":[[[0,0],[2,2],[0,2],[2,0],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,1],[2,2],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[2,0],[2,2],[0,0],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[2,0],[1,0],[1,2],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]],[[0,0]]]}`,
		`{"type":"Feature","coordinates":[]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[181,0],[1,1],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,86],[1,1],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[[[179,0],[-179,0],[-179,1],[179,0]]]}`,
		`12/1/1`,
		`{"type":"Polygon","coordinates":[[[null,0],[1,0],[1,1],[0,0]]]}`,
	} {
		name := filepath.Join(t.TempDir(), "bad.geojson")
		os.WriteFile(name, []byte(data), 0600)
		if _, err := readContour(name); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestCLIFromCachedTiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "contour.geojson")
	config := filepath.Join(dir, "layers.yml")
	output := filepath.Join(dir, "result.mbtiles")
	os.WriteFile(file, []byte(`{"type":"Polygon","coordinates":[[[-1,-1],[1,-1],[1,1],[-1,1],[-1,-1]]]}`), 0600)
	os.WriteFile(config, []byte("- key: test\n  name: Test\n  minZoom: 0\n  maxZoom: 4\n  tileType: png\n  url: invalid://no-network\n"), 0600)
	args := []string{"-minZ", "1", "-maxZ", "2", "-layer", "test", "-layers", config, "-path", dir, "-map_name", output}
	var out bytes.Buffer
	if err := run(context.Background(), append(append([]string{}, args...), "-dry-run", file), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Total") || !strings.Contains(out.String(), "no network sampling") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("dry run created output")
	}
	p, _ := readContour(file)
	plans, _ := p.plan(context.Background(), 1, 2)
	for _, plan := range plans {
		err := p.walk(context.Background(), plan, func(tile tile) error {
			name := filepath.Join(dir, "tiles", "test", fmt.Sprintf("z%d/0/x%d/0/y%d.png", tile.z, tile.x, tile.y))
			if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
				return err
			}
			return os.WriteFile(name, []byte(fmt.Sprintf("tile:%d/%d/%d", tile.z, tile.x, tile.y)), 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	if err := run(context.Background(), append(args, file), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Index(out.String(), "Total") > strings.Index(out.String(), "Saved") {
		t.Fatal("statistics not printed first")
	}
	db, err := sql.Open("sqlite", output)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM tiles").Scan(&count); err != nil || count != 8 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var data []byte
	if err := db.QueryRow("SELECT tile_data FROM tiles WHERE zoom_level=2 AND tile_column=1 AND tile_row=2").Scan(&data); err != nil || string(data) != "tile:2/1/1" {
		t.Fatalf("TMS row conversion: %q %v", data, err)
	}
	for name, want := range map[string]string{"scheme": "tms", "format": "png", "minzoom": "1", "maxzoom": "2"} {
		var got string
		if err := db.QueryRow("SELECT value FROM metadata WHERE name=?", name).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%s err=%v", name, got, err)
		}
	}
	before, _ := os.ReadFile(output)
	if err := run(context.Background(), append(args, file), io.Discard, io.Discard); err == nil {
		t.Fatal("overwrote existing output")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(before, after) {
		t.Fatal("changed existing output")
	}
	for _, bad := range [][]string{{"-minZ", "2", "-maxZ", "1"}, {"-minZ", "1", "-maxZ", "2", "-n", "0"}, {"-minZ", "1", "-maxZ", "2", "-tile-size-kib", "NaN"}} {
		if err := run(context.Background(), append(bad, file), io.Discard, io.Discard); err == nil {
			t.Fatal("accepted invalid flags")
		}
	}
}

type failingSource struct {
	model.Source
	calls atomic.Int64
}

func (*failingSource) GetContentType() string { return "image/png" }
func (s *failingSource) GetTile(context.Context, int, int, int) (string, []byte, error) {
	s.calls.Add(1)
	return "", nil, fmt.Errorf("download failed")
}
func TestDownloadFailureLeavesNoOutput(t *testing.T) {
	p := contour{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	plans, _ := p.plan(ctx, 1, 2)
	dir := t.TempDir()
	output := filepath.Join(dir, "result.mbtiles")
	source := new(failingSource)
	if err := download(ctx, source, p, plans, output, "test", 2, 3, io.Discard); err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("error=%v", err)
	}
	if source.calls.Load() == 0 {
		t.Fatal("no downloads attempted")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatalf("left incomplete output: %v", files)
	}
}

type retrySource struct {
	model.Source
	mu       sync.Mutex
	calls    []tile
	attempts map[tile]int
	fail     func(tile, int) bool
	cancel   context.CancelFunc
}

func (*retrySource) GetContentType() string { return "image/png" }
func (s *retrySource) GetTile(ctx context.Context, z, x, y int) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := tile{z, x, y}
	if s.attempts == nil {
		s.attempts = make(map[tile]int)
	}
	s.calls = append(s.calls, t)
	s.attempts[t]++
	if s.cancel != nil {
		s.cancel()
		return "", nil, ctx.Err()
	}
	if s.fail(t, s.attempts[t]) {
		return "", nil, fmt.Errorf("temporary failure")
	}
	return "image/png", []byte("tile"), nil
}

func TestRetriesGoToQueueTail(t *testing.T) {
	p := contour{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
	plans, _ := p.plan(context.Background(), 1, 1)
	var initial []tile
	_ = p.walk(context.Background(), plans[0], func(t tile) error { initial = append(initial, t); return nil })
	source := &retrySource{fail: func(t tile, attempt int) bool { return t == initial[0] && attempt == 1 }}
	output := filepath.Join(t.TempDir(), "result.mbtiles")
	if err := download(context.Background(), source, p, plans, output, "test", 1, 3, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := append(append([]tile{}, initial...), initial[0])
	if !reflect.DeepEqual(source.calls, want) {
		t.Fatalf("calls %v, want %v", source.calls, want)
	}
	db, err := sql.Open("sqlite", output)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM tiles").Scan(&count); err != nil || count != len(initial) {
		t.Fatalf("tiles=%d error=%v", count, err)
	}
}

func TestRetryLimitAndCancellation(t *testing.T) {
	p := contour{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
	plans, _ := p.plan(context.Background(), 1, 1)
	for _, retries := range []int{0, 3} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			source := &retrySource{fail: func(tile, int) bool { return true }}
			dir := t.TempDir()
			err := download(context.Background(), source, p, plans, filepath.Join(dir, "out.mbtiles"), "test", 1, retries, io.Discard)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("after %d attempts", retries+1)) {
				t.Fatalf("error=%v", err)
			}
			if source.attempts[source.calls[0]] != retries+1 {
				t.Fatal(source.attempts)
			}
			for _, attempts := range source.attempts {
				if attempts > retries+1 {
					t.Fatal(source.attempts)
				}
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatalf("incomplete output remains: %v", files)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &retrySource{cancel: cancel}
	dir := t.TempDir()
	if err := download(ctx, source, p, plans, filepath.Join(dir, "out.mbtiles"), "test", 2, 3, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
