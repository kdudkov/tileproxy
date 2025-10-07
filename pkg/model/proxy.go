package model

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

var _ Source = &Proxy{}

type Proxy struct {
	logger      *slog.Logger
	name        string
	key         string
	minZoom     int
	maxZoom     int
	tms         bool
	path        string
	url         string
	ext         string
	serverParts []string
	timeout     time.Duration
	httpTimeout time.Duration
	cl          *http.Client

	urlGetter func(z, x, y int) string

	Offline         bool
	keepProbability float32

	t1 *Tile
	t2 *Tile
}

func (p *Proxy) Init() {
	p.cl = &http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			ResponseHeaderTimeout: p.httpTimeout,
			//MaxConnsPerHost:       4,
		},
	}
}

func (p *Proxy) GetName() string {
	return p.name
}

func (p *Proxy) GetKey() string {
	return p.key
}

func (p *Proxy) GetMinZoom() int {
	return p.minZoom
}

func (p *Proxy) GetMaxZoom() int {
	return p.maxZoom
}

func (p *Proxy) GetContentType() string {
	switch p.ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	default:
		return "image/png"
	}
}

func (p *Proxy) IsTms() bool {
	return p.tms
}

func (p *Proxy) IsFile() bool {
	return false
}

func (p *Proxy) GetTile(ctx context.Context, z, x, y int) ([]byte, error) {
	if z < p.minZoom || z > p.maxZoom {
		return nil, fmt.Errorf("invalid zoom")
	}

	if p.t1 != nil && p.t2 != nil && !(&Tile{X: x, Y: y, Z: z}).InRect(p.t1, p.t2) {
		return nil, fmt.Errorf("border")
	}

	if p.tms {
		y = 1<<z - y - 1
	}

	logger := p.logger.With("zoom", strconv.Itoa(z))

	fpath := path.Join(p.path, fmt.Sprintf("z%d/%d/x%d/%d", z, int(x/1024), x, int(y/1024)))
	fname := fmt.Sprintf("y%d.%s", y, p.ext)

	st, err := os.Stat(path.Join(fpath, fname))

	if err != nil {
		logger.Debug("miss")
		return p.download(ctx, p.GetUrl(z, x, y), fpath, fname)
	}

	if p.timeout == 0 || st.ModTime().Add(p.timeout).After(time.Now()) {
		logger.Debug("hit")
		return os.ReadFile(path.Join(fpath, fname))
	}

	if rand.Float32() < p.keepProbability {
		logger.Debug("keep")
		return os.ReadFile(path.Join(fpath, fname))
	}

	logger.Debug("timeout")
	data, err := p.download(ctx, p.GetUrl(z, x, y), fpath, fname)

	// backup - return file if any
	if err != nil {
		return os.ReadFile(path.Join(fpath, fname))
	}

	return data, nil
}

func (p *Proxy) download(ctx context.Context, url string, fpath, fname string) ([]byte, error) {
	if p.Offline {
		return nil, fmt.Errorf("offline")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)

	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:125.0) Gecko/20100101 Firefox/125.0")

	resp, err := p.cl.Do(req)

	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s error %s\n", url, resp.Status)
	}

	if resp.Body == nil {
		return nil, fmt.Errorf("nil body")
	}

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(fpath, 0755); err != nil {
		return nil, err
	}

	fl, err := os.Create(path.Join(fpath, fname))

	if err != nil {
		return nil, err
	}

	if _, err = fl.Write(data); err != nil {
		return data, err
	}

	fl.Close()

	return data, nil
}

func (p *Proxy) GetUrl(z, x, y int) string {
	if p.urlGetter == nil {
		url := strings.ReplaceAll(p.url, "{z}", strconv.Itoa(z))
		url = strings.ReplaceAll(url, "{x}", strconv.Itoa(x))
		url = strings.ReplaceAll(url, "{y}", strconv.Itoa(y))

		if len(p.serverParts) > 0 {
			i := rand.Intn(len(p.serverParts))
			url = strings.ReplaceAll(url, "{s}", p.serverParts[i])
		}

		return url
	}

	return p.urlGetter(z, x, y)
}
