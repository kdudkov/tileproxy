package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"slices"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"

	"github.com/kdudkov/tileproxy/pkg/model"
)

type App struct {
	addr     string
	filesDir string
	cacheDir string
	logger   *slog.Logger
	layers   *Layers
}

func NewApp(addr string) *App {
	return &App{
		layers: NewLayers(),
		logger: slog.Default(),
		addr:   addr,
	}
}

func (app *App) addDefaultSources() error {
	d, err := os.ReadFile("layers.yml")

	if err != nil {
		return err
	}

	var res []*model.LayerDescription

	if err := yaml.Unmarshal(d, &res); err != nil {
		return err
	}

	for _, l := range res {
		p := model.NewProxy(l, app.logger, app.cacheDir)
		app.layers.Add(p)
	}

	return nil
}

func (app *App) addFileSources() error {
	files, err := os.ReadDir(app.filesDir)
	if err != nil {
		return err
	}

	sources := make([]model.Source, 0, len(files))

	for _, f := range files {
		p := path.Join(app.filesDir, f.Name())

		if f.IsDir() {
			layer, err := app.addMultiFiles(f.Name(), p)
			if err != nil {
				app.logger.Error("load multilayer", "error", err)
			} else if layer != nil {
				sources = append(sources, layer)
			}
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

		sources = append(sources, l)
		app.logger.Info(fmt.Sprintf("loaded file %s, %s", f.Name(), l.String()))
	}

	app.layers.ReplaceFiles(sources)
	return nil
}

func (app *App) addMultiFiles(name, dpath string) (model.Source, error) {
	files, err := os.ReadDir(dpath)
	if err != nil {
		return nil, err
	}

	layers := make([]*model.Layer, 0)

	for _, f := range files {
		p := path.Join(dpath, f.Name())

		if f.IsDir() {
			continue
		}

		if !strings.HasSuffix(f.Name(), ".mbtiles") && !strings.HasSuffix(f.Name(), ".sqlite") {
			continue
		}

		l, err := model.NewLayer(f.Name(), p)

		if err != nil {
			app.logger.Error("db open error", "error", err)
			continue
		}

		layers = append(layers, l)
	}

	if len(layers) == 0 {
		return nil, nil
	}

	slices.SortFunc(layers, func(l1, l2 *model.Layer) int {
		return strings.Compare(l1.GetName(), l2.GetName())
	})

	layer := model.NewMultilayer(name, name, layers)
	app.logger.Info(fmt.Sprintf("loaded multilayer %s, %d files", name, len(layers)))

	return layer, nil
}

func (app *App) Run() {
	defer app.close()
	if err := os.MkdirAll(app.cacheDir, 0777); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(app.filesDir, 0777); err != nil {
		panic(err)
	}
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

	watchDone := make(chan struct{})
	go func() { defer close(watchDone); app.watch(watcher) }()

	err = watcher.Add(app.filesDir)
	if err != nil {
		panic(err)
	}

	app.loop()
	_ = watcher.Close()
	<-watchDone
	if err := http.Shutdown(); err != nil {
		app.logger.Error("shutdown HTTP", "error", err)
	}
}

func (app *App) watch(watcher *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			app.logger.Info(fmt.Sprintf("event: %s, reload files", event))
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
	app.layers.Clear()
}

func (app *App) loop() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	<-sigc
}

func main() {
	var filesDir = flag.String("files", "./data", "mbtiles path")
	var cacheDir = flag.String("cache", "./data", "cache path")
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
