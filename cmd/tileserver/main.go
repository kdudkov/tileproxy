package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"go.uber.org/zap"

	"github.com/kdudkov/tileproxy/pkg/model"
)

type App struct {
	addr   string
	logger *zap.SugaredLogger
	layers map[string]model.Source
}

func NewApp(logger *zap.SugaredLogger, addr string) *App {
	return &App{
		layers: make(map[string]model.Source),
		logger: logger,
		addr:   addr,
	}
}

func (app *App) addFefaultSources(path string) {
	for _, p := range []model.Source{model.GoogleHybrid(app.logger, path), model.OpenTopoCZ(app.logger, path)} {
		app.layers[p.GetKey()] = p
	}
}

func (app *App) Run(rootPath string) {
	app.addFefaultSources(rootPath)

	files, err := os.ReadDir(rootPath)
	if err != nil {
		panic(err)
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
			fmt.Printf("invalid file: %s\n", p)
			continue
		}

		l, err := model.NewLayer(f.Name(), p)
		if err != nil {
			fmt.Printf("db open error: %v\n", err)
			continue
		}

		app.layers[l.GetKey()] = l

		app.logger.Infof("got layer %s", l)
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
