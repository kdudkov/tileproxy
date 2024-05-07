package model

import (
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

type LayerDescription struct {
	Name            string        `yaml:"name"`
	Key             string        `yaml:"key"`
	MinZoom         int           `yaml:"minZoom"`
	MaxZoom         int           `yaml:"maxZoom"`
	Tms             bool          `yaml:"tms"`
	Url             string        `yaml:"url"`
	TileType        string        `yaml:"tileType"`
	ServerParts     []string      `yaml:"serverParts"`
	Timeout         time.Duration `yaml:"timeout"`
	KeepProbability float32       `yaml:"keepProbability"`
}

func NewProxy(l *LayerDescription, logger *slog.Logger, path string) *Proxy {
	return &Proxy{
		logger:          logger,
		minZoom:         l.MinZoom,
		maxZoom:         l.MaxZoom,
		keepProbability: l.KeepProbability,
		key:             l.Key,
		name:            l.Name,
		tms:             l.Tms,
		path:            filepath.Join(path, "tiles", l.Key),
		url:             l.Url,
		ext:             strings.ToLower(l.TileType),
		serverParts:     l.ServerParts,
		timeout:         l.Timeout,
		httpTimeout:     time.Second * 10,
	}
}
