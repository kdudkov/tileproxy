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
	"sync"

	"github.com/schollz/progressbar/v3"
	"gopkg.in/yaml.v3"

	_ "modernc.org/sqlite"

	"github.com/kdudkov/tileproxy/pkg/model"
)

type App struct {
	logger        *slog.Logger
	layer         model.Source
	dbFilename    string
	tilesFilename string
	title         string
	workers       int
}

func NewApp(l model.Source, dbFilename, tilesFilename, title string, workers int) *App {
	return &App{
		logger:        slog.Default(),
		layer:         l,
		dbFilename:    dbFilename,
		tilesFilename: tilesFilename,
		title:         title,
		workers:       workers,
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

	ch := make(chan string)
	fnchan := make(chan func(db *sql.DB))

	wg := new(sync.WaitGroup)

	bar := progressbar.Default(getTilesNum(app.tilesFilename), "tiles downloaded")

	wg.Add(1)
	go dbWorker(wg, db, fnchan, bar)

	wg1 := new(sync.WaitGroup)
	for i := 0; i < app.workers; i++ {
		wg1.Add(1)
		go app.worker(i, wg1, ch, fnchan)
	}

	for {
		ln, readerr := r.ReadString('\n')

		if readerr != nil && !errors.Is(readerr, io.EOF) {
			return readerr
		}

		if ln != "" {
			ch <- ln
		}

		if errors.Is(readerr, io.EOF) {
			break
		}
	}

	close(ch)
	wg1.Wait()
	close(fnchan)
	wg.Wait()

	zmin, zmax, err := getZoom(db)

	if err != nil {
		return err
	}

	meta := map[string]string{
		"version": "1.1",
		"format":  app.GetType(),
		"minzoom": strconv.Itoa(zmin),
		"maxzoom": strconv.Itoa(zmax),
		"name":    app.title,
		"scheme":  "tms",
	}

	if err := putMeta(db, meta); err != nil {
		return err
	}

	fmt.Printf("zoom: %d - %d\n", zmin, zmax)

	return nil
}

func getTilesNum(name string) int64 {
	f, err := os.Open(name)

	if err != nil {
		return -1
	}

	defer f.Close()

	r := bufio.NewReader(f)

	var res int64

	for {
		ln, readerr := r.ReadString('\n')

		if readerr != nil && !errors.Is(readerr, io.EOF) {
			return -1
		}

		if ln != "" {
			res++
		}

		if errors.Is(readerr, io.EOF) {
			break
		}
	}

	return res
}

func LoadSources(logger *slog.Logger, cacheDir string) ([]*model.Proxy, error) {
	d, err := os.ReadFile("layers.yml")

	if err != nil {
		return nil, err
	}

	var res []*model.LayerDescription

	if err := yaml.Unmarshal(d, &res); err != nil {
		return nil, err
	}

	layers := make([]*model.Proxy, 0, len(res))

	for _, l := range res {
		p := model.NewProxy(l, logger, cacheDir)
		layers = append(layers, p)
	}

	return layers, nil
}

func dbWorker(wg *sync.WaitGroup, db *sql.DB, ch chan func(db *sql.DB), bar *progressbar.ProgressBar) {
	for fn := range ch {
		bar.Add(1)
		fn(db)
	}

	wg.Done()
}

func (app *App) worker(i int, wg *sync.WaitGroup, ch chan string, fnchan chan func(db *sql.DB)) {
	ctx := context.Background()

	logger := app.logger.With("worker", strconv.Itoa(i))

	for s := range ch {
		d := strings.Split(strings.Trim(s, "\n\r "), "/")

		if len(d) != 3 {
			logger.Error("invalid string: " + s)
			continue
		}

		z, _ := strconv.Atoi(d[0])
		x, _ := strconv.Atoi(d[1])
		y, _ := strconv.Atoi(d[2])

		data, err := app.layer.GetTile(ctx, z, x, y)

		if err != nil {
			logger.Error("error", "error", err)
			continue
		}

		if data == nil {
			logger.Error("nil data")
			continue
		}

		fnchan <- func(db *sql.DB) {
			if err := putData(db, z, x, y, data); err != nil {
				logger.Error("save error", "error", err)
			}
		}
	}

	logger.Info("done")
	wg.Done()
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

func getZoom(db *sql.DB) (int, int, error) {
	row, err := db.Query("select max(zoom_level) as zmax, min(zoom_level) as zmin FROM tiles")

	if err != nil {
		return 0, 0, err
	}

	defer row.Close()

	var zmin, zmax int

	if row.Next() {
		err1 := row.Scan(&zmax, &zmin)
		return zmin, zmax, err1
	}

	return 0, 0, nil
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
	var mapName = flag.String("map_name", "", "")
	var flagTitle = flag.String("title", "", "")
	var workers = flag.Int("n", 2, "")

	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Println("no file name")
		return
	}

	tilesFile := flag.Arg(0)
	dbFile := *mapName

	if dbFile == "" {
		dbFile = fmt.Sprintf("%s_%s.mbtiles", strings.Trim(tilesFile, "./"), *layer)
	}

	if !strings.HasSuffix(dbFile, ".mbtiles") {
		dbFile = dbFile + ".mbtiles"
	}

	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(h))

	layers, err := LoadSources(slog.Default(), *dir)

	if err != nil {
		fmt.Println(err)
		return
	}

	var proxy model.Source

	for _, s := range layers {
		if s.GetKey() == *layer {
			proxy = s
			break
		}
	}

	if proxy == nil {
		fmt.Println("you need to specify a valid proxy")
		return
	}

	title := *flagTitle

	if title == "" {
		title = fmt.Sprintf("%s %s", proxy.GetName(), strings.Trim(tilesFile, "./"))
	}

	err = NewApp(proxy, dbFile, tilesFile, title, *workers).Run()

	if err != nil {
		fmt.Printf("error: %s\n", err.Error())
	}
}
