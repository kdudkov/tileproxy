package main

import (
	"fmt"
	"testing"
)

var testdata = [][]float64{
	{60.2, 30.17, 6701433.35, 3525420.53},
}

func TestConvert(t *testing.T) {
	//ts := TileSystem{isTms: false}

	//for i, c := range testdata {
	//	t.Run(fmt.Sprintf("test_%d", i), func(t *testing.T) {
	//		x, y := ts.latlon2xy(c[0], c[1])
	//
	//		if x != c[2] {
	//			t.Errorf("wrong x: got %f, must be %f", x, c[2])
	//		}
	//
	//		if y != c[3] {
	//			t.Errorf("wrong y: got %f, must be %f", y, c[3])
	//		}
	//	})
	//}
}

func Test2(t *testing.T) {
	ts := NewTileSystem()
	lat, lon := 55.746819, 37.612228
	zoom := 16
	xt, yt, _, _ := ts.latlon2tilexy(lat, lon, zoom)

	fmt.Printf("https://a.tile.openstreetmap.org/%d/%d/%d.png'\n", zoom, xt, yt)

}
