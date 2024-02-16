package main

import (
	"flag"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/kdudkov/tileproxy/pkg/model"
)

type App struct {
	addr   string
	logger *zap.SugaredLogger
	layers []model.Source
}

func NewApp(logger *zap.SugaredLogger, addr string) *App {
	return &App{
		layers: nil,
		logger: logger,
		addr:   addr,
	}
}

func (app *App) addDefaultSources(path string) error {
	d, err := os.ReadFile("layers.yml")

	if err != nil {
		return err
	}

	var res []*model.LayerDescription

	if err := yaml.Unmarshal(d, &res); err != nil {
		return err
	}

	for _, l := range res {
		p := model.NewProxy(l, app.logger, path)
		app.layers = append(app.layers, p)
	}

	return nil
}

func (app *App) addFileSources(rootPath string) error {
	files, err := os.ReadDir(rootPath)
	if err != nil {
		return err
	}

	for _, f := range files {
		p := path.Join(rootPath, f.Name())
		if f.IsDir() {
			continue
		}

		if !strings.HasSuffix(f.Name(), ".mbtiles") && !strings.HasSuffix(f.Name(), ".sqlite") {
			continue
		}

		if _, err := os.Stat(p); err != nil {
			app.logger.Errorf("invalid file %s: %v", p, err)
			continue
		}

		l, err := model.NewLayer(f.Name(), p)
		if err != nil {
			app.logger.Errorf("db open error: %v", err)
			continue
		}

		app.layers = append(app.layers, l)
	}

	return nil
}

func (app *App) Run(rootPath string) {
	if err := app.addDefaultSources(rootPath); err != nil {
		panic(err)
	}

	if err := app.addFileSources(rootPath); err != nil {
		panic(err)
	}

	http := NewHttp(app)

	app.logger.Infof("listening on %s", app.addr)

	go func() {
		if err := http.Listen(app.addr); err != nil {
			panic(err)
		}
	}()

	app.loop()
	app.close()
}

func (app *App) close() {

}

func (app *App) loop() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	<-sigc
}

func main() {
	var dir = flag.String("path", ".", "mbtiles path")
	var addr = flag.String("addr", "localhost:8080", "listen address")
	var debug = flag.Bool("debug", false, "")

	flag.Parse()

	var cfg zap.Config
	if *debug {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
		cfg.Encoding = "console"
	}

	logger, _ := cfg.Build()
	defer logger.Sync()

	app := NewApp(logger.Sugar(), *addr)
	app.Run(*dir)
}
