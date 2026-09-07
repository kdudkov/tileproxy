package model

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

func TestDownloadClosesResponseBody(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := &trackedBody{Reader: strings.NewReader("response")}
			p := &Proxy{cl: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: body}, nil
			})}}
			dir := t.TempDir()
			_, err := p.download(context.Background(), "https://tiles.example/tile", dir, "tile.png")
			if (err != nil) != (status >= 300) {
				t.Fatalf("status %d: unexpected error %v", status, err)
			}
			if !body.closed {
				t.Fatal("response body was not closed")
			}
			if status >= 300 {
				if _, err := os.Stat(filepath.Join(dir, "tile.png")); !os.IsNotExist(err) {
					t.Fatalf("error response was cached: %v", err)
				}
			}
		})
	}
}

func TestQueuedURLIsPreservedAcrossRetries(t *testing.T) {
	var urls []string
	p := &Proxy{minZoom: 0, maxZoom: 2, tms: true, path: t.TempDir(), ext: "png", logger: slog.Default()}
	p.cl = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		status := http.StatusOK
		if len(urls) == 1 {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader("tile"))}, nil
	})}
	url := "https://tiles.example/2/1/2.png"
	if _, _, err := p.GetTileFromURL(context.Background(), 2, 1, 1, url); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	if _, _, err := p.GetTileFromURL(context.Background(), 2, 1, 1, url); err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 || urls[0] != url || urls[1] != url {
		t.Fatal(urls)
	}
	if _, err := os.Stat(filepath.Join(p.path, "z2/0/x1/0/y2.png")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.GetTileFromURL(context.Background(), 2, 1, 1, url); err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 {
		t.Fatal("cache hit repeated HTTP request")
	}
}

func TestCacheReplacementIsAtomic(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "tile.png")
	if err := os.WriteFile(name, []byte("old tile"), 0644); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := writeCacheFile(name, []byte("new tile")); err != nil {
		t.Fatal(err)
	}
	old, err := io.ReadAll(reader)
	if err != nil || string(old) != "old tile" {
		t.Fatalf("active reader saw %q: %v", old, err)
	}
	current, err := os.ReadFile(name)
	if err != nil || string(current) != "new tile" {
		t.Fatalf("new reader saw %q: %v", current, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files remain: %v", entries)
	}
}

func TestFailedCachePublishRemovesTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tile.png")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeCacheFile(target, []byte("tile")); err == nil {
		t.Fatal("expected rename to fail")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "tile.png" || !entries[0].IsDir() {
		t.Fatalf("destination changed or temporary files remain: %v", entries)
	}
}
