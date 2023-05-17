package main

import (
	"math"
)

func radians(a float64) float64 {
	return a / 180 * math.Pi
}

func deg(a float64) float64 {
	return a / math.Pi * 180
}

type TileSystem struct {
	isTms    bool
	tileSize int
}

func NewTileSystem() *TileSystem {
	return &TileSystem{
		isTms:    false,
		tileSize: 128,
	}
}
func (ts *TileSystem) latlon2xy(lat, lon float64, zoom int) (int, int) {
	size := 1 << zoom * ts.tileSize

	x := (lon + 180) / 360 * float64(size)
	y := (1 - math.Log(math.Tan(radians(lat))+(1/math.Cos(radians(lat))))/math.Pi) / 2 * float64(size)
	if ts.isTms {
		y = float64(size) - y
	}
	return int(math.Round(x)), int(math.Round(y))
}

func (ts *TileSystem) latlon2tilexy(lat, lon float64, zoom int) (int, int, int, int) {
	x, y := ts.latlon2xy(lat, lon, zoom)

	return x / ts.tileSize, y / ts.tileSize, x % ts.tileSize, y % ts.tileSize
}

func (ts *TileSystem) xy2latlon(x, y float64, zoom int) (float64, float64) {
	size := 1 << zoom * ts.tileSize
	if ts.isTms {
		y = float64(size) - y
	}
	lon := x/float64(size)*360.0 - 180.0
	lat := deg(math.Atan(math.Sinh(math.Pi * (1 - 2*y/float64(size)))))
	return lat, lon
}
