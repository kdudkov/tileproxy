package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"

	"github.com/kdudkov/tileproxy/pkg/model"
)

type App struct {
	addr       string
	filesDir   string
	cacheDir   string
	logger     *slog.Logger
	mx         sync.RWMutex
	layers     []model.Source
	fileLayers []model.Source
}

func NewApp(addr string) *App {
	return &App{
		layers:     nil,
		fileLayers: nil,
		logger:     slog.Default(),
		addr:       addr,
		mx:         sync.RWMutex{},
	}
}

func (app *App) addDefaultSources() error {
	app.mx.Lock()
	defer app.mx.Unlock()

	d, err := os.ReadFile("layers.yml")

	if err != nil {
		return err
	}

	var res []*model.LayerDescription

	if err := yaml.Unmarshal(d, &res); err != nil {
		return err
	}

	app.layers = make([]model.Source, 0, len(res))

	for _, l := range res {
		p := model.NewProxy(l, app.logger, app.cacheDir)
		app.layers = append(app.layers, p)
	}

	return nil
}

func (app *App) addFileSources() error {
	app.mx.Lock()
	defer app.mx.Unlock()

	files, err := os.ReadDir(app.filesDir)
	if err != nil {
		return err
	}

	app.fileLayers = make([]model.Source, 0)

	for _, f := range files {
		p := path.Join(app.filesDir, f.Name())
		if f.IsDir() {
			continue
		}

		if !strings.HasSuffix(f.Name(), ".mbtiles") && !strings.HasSuffix(f.Name(), ".sqlite") {
			continue
		}

		if _, err := os.Stat(p); err != nil {
			app.logger.Error("invalid file "+p, "error", err)
			continue
		}

		l, err := model.NewLayer(f.Name(), p)
		if err != nil {
			app.logger.Error("db open error", "error", err)
			continue
		}

		app.fileLayers = append(app.fileLayers, l)
		app.logger.Info(fmt.Sprintf("loaded file %s, name %s", f.Name(), l.GetName()))
	}

	return nil
}

func (app *App) Run() {
	if err := app.addDefaultSources(); err != nil {
		panic(err)
	}

	if err := app.addFileSources(); err != nil {
		panic(err)
	}

	http := NewHttp(app)

	app.logger.Info("listening on " + app.addr)

	go func() {
		if err := http.Listen(app.addr); err != nil {
			panic(err)
		}
	}()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	defer watcher.Close()

	go app.watch(watcher)

	err = watcher.Add(app.filesDir)
	if err != nil {
		panic(err)
	}

	app.loop()
	app.close()
}

func (app *App) watch(watcher *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			app.logger.Info(fmt.Sprintf("event: %s", event))
			if event.Has(fsnotify.Write) {
				app.logger.Info("modified file: " + event.Name)
			}

			if err := app.addFileSources(); err != nil {
				app.logger.Error("error", slog.Any("error", err))
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			app.logger.Error("error", slog.Any("error", err))
		}
	}
}

func (app *App) close() {

}

func (app *App) loop() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	<-sigc
}

func main() {
	var filesDir = flag.String("files", ".", "mbtiles path")
	var cacheDir = flag.String("cache", ".", "cache path")
	var addr = flag.String("addr", ":8888", "listen address")
	var debug = flag.Bool("debug", false, "")

	flag.Parse()

	var h slog.Handler
	if *debug {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	slog.SetDefault(slog.New(h))

	app := NewApp(*addr)
	app.filesDir = *filesDir
	app.cacheDir = *cacheDir
	app.Run()
}
