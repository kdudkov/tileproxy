package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/kdudkov/tileproxy/pkg/model"
	_ "modernc.org/sqlite"
)

type App struct {
	layer         model.Source
	dbFilename    string
	tilesFilename string
}

func NewApp(l model.Source, dbFilename, tilesFilename string) *App {
	return &App{
		layer:         l,
		dbFilename:    dbFilename,
		tilesFilename: tilesFilename,
	}
}

func (app *App) GetType() string {
	switch app.layer.GetContentType() {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	default:
		return "png"
	}
}

func (app *App) Run() error {
	if app.layer == nil {
		fmt.Println("no layer!")
		return nil
	}

	_ = os.Remove(app.dbFilename)
	db, err := sql.Open("sqlite", app.dbFilename)

	if err != nil {
		return err
	}

	defer db.Close()

	if err := createTables(db); err != nil {
		return err
	}

	fmt.Printf("start reading file %s\n", app.tilesFilename)
	f, err := os.Open(app.tilesFilename)

	if err != nil {
		return err
	}

	defer f.Close()

	r := bufio.NewReader(f)

	minzoom, maxzoom := 0, 0
	total := 0

	ctx := context.Background()

	for {
		ln, readerr := r.ReadString('\n')

		if readerr != nil && !errors.Is(readerr, io.EOF) {
			return readerr
		}

		if ln != "" {
			d := strings.Split(strings.Trim(ln, "\n\r "), "/")

			if len(d) != 3 {
				return fmt.Errorf("invalid string: %s", ln)
			}

			z, _ := strconv.Atoi(d[0])
			x, _ := strconv.Atoi(d[1])
			y, _ := strconv.Atoi(d[2])

			data, err := app.layer.GetTile(ctx, z, x, y)

			if err != nil {
				return err
			}

			if data == nil {
				return fmt.Errorf("no tile z=%d %d/%d", z, x, y)
			}

			if err := putData(db, z, x, y, data); err != nil {
				return err
			}

			total += 1

			if minzoom == 0 {
				minzoom = z
			}

			minzoom = min(minzoom, z)
			maxzoom = max(maxzoom, z)
		}

		if errors.Is(readerr, io.EOF) {
			break
		}
	}

	meta := map[string]string{
		"version": "1.1",
		"format":  app.GetType(),
		"minzoom": fmt.Sprintf("%d", minzoom),
		"maxzoom": fmt.Sprintf("%d", maxzoom),
		"name":    app.tilesFilename,
		"scheme":  "tms",
	}

	if err := putMeta(db, meta); err != nil {
		return err
	}

	fmt.Printf("zoom: %d - %d\n", minzoom, maxzoom)
	fmt.Printf("total tiles: %d\n", total)

	return nil
}

func createTables(db *sql.DB) error {
	_, err := db.Exec("CREATE TABLE IF NOT EXISTS tiles (zoom_level INTEGER NOT NULL,tile_column INTEGER NOT NULL,tile_row INTEGER NOT NULL,tile_data BLOB NOT NULL,UNIQUE (zoom_level, tile_column, tile_row));")

	if err != nil {
		return err
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS metadata (name TEXT, value TEXT);")

	return err
}

func putData(db *sql.DB, z, x, y int, data []byte) error {
	y1 := 1<<z - y - 1

	_, err := db.Exec("INSERT INTO tiles (zoom_level, tile_column, tile_row, tile_data) values (?,?,?,?)", z, x, y1, data)
	return err
}

func putMeta(db *sql.DB, meta map[string]string) error {
	for k, v := range meta {
		_, err := db.Exec("INSERT INTO metadata (name, value) values (?,?)", k, v)
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	var dir = flag.String("path", ".", "mbtiles path")
	var layer = flag.String("layer", "", "layer")

	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Println("no file name")
		return
	}

	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))

	var proxy model.Source

	switch *layer {
	case "google_h", "google":
		proxy = model.GoogleHybrid(slog.Default(), *dir)
	case "topo":
		proxy = model.OpenTopoCZ(slog.Default(), *dir)
	default:
		fmt.Println("you need to specify a proxy: google or topo")
		return
	}

	err := NewApp(proxy, flag.Arg(0)+".mbtiles", flag.Arg(0)).Run()

	if err != nil {
		fmt.Printf("error: %s\n", err.Error())
	}
}
