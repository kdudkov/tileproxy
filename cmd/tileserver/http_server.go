package main

import (
	"embed"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/redirect"
	"github.com/gofiber/template/html/v2"

	"github.com/kdudkov/tileproxy/pkg/model"
)

//go:embed template/*
var templates embed.FS

//go:embed static/*
var embedDirStatic embed.FS

func NewHttp(app *App) *fiber.App {
	engine := html.NewFileSystem(http.FS(templates), ".html")
	engine.Delims("[[", "]]")

	f := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		EnablePrintRoutes:     false,
		Views:                 engine,
	})

	f.Use(logger.New(logger.Config{
		Format: "[${ip}]:${port} ${status} - ${locals:username} ${method} ${path} ${queryParams}\n",
	}))

	f.Use(cors.New(cors.Config{
		AllowOrigins: "*",
	}))

	f.Use(redirect.New(redirect.Config{
		Rules: map[string]string{
			"/map": "/static/index.html",
		},
		StatusCode: 302,
	}))

	f.Get("/", getIndexHandler(app))
	f.Get("/layers", getLayersHandler(app))
	f.Get("/tiles/:name/:zoom/:x/:y", getTileHandler(app))

	f.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(embedDirStatic),
		PathPrefix: "static",
	}))

	return f
}

func getIndexHandler(app *App) func(c *fiber.Ctx) error {
	addrs := getLocalAddr()

	return func(c *fiber.Ctx) error {
		_, port, err := net.SplitHostPort(app.addr)

		if err != nil {
			return err
		}

		d := fiber.Map{
			"port":   port,
			"ips":    addrs,
			"layers": app.getLayers(),
		}

		return c.Render("template/index", d, "template/_header")

	}
}

func getLayersHandler(app *App) func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.JSON(app.getLayers())
	}
}

func (app *App) getLayers() []map[string]any {
	r := make([]map[string]any, 0)

	app.mx.RLock()
	defer app.mx.RUnlock()

	for _, l := range app.layers {
		ld := make(map[string]any)
		ld["url"] = "/tiles/" + url.QueryEscape(l.GetKey()) + "/{z}/{x}/{y}"
		ld["min_zoom"] = l.GetMinZoom()
		ld["max_zoom"] = l.GetMaxZoom()
		ld["name"] = l.GetName()
		ld["file"] = l.IsFile()
		r = append(r, ld)
	}

	for _, l := range app.fileLayers {
		ld := make(map[string]any)
		ld["url"] = "/tiles/" + url.QueryEscape(l.GetKey()) + "/{z}/{x}/{y}"
		ld["min_zoom"] = l.GetMinZoom()
		ld["max_zoom"] = l.GetMaxZoom()
		ld["name"] = l.GetName()
		ld["file"] = l.IsFile()
		r = append(r, ld)
	}

	return r
}

func getTileHandler(app *App) func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		var err error
		var zoom, x, y int

		name, _ := url.QueryUnescape(c.Params("name"))

		if zoom, err = c.ParamsInt("zoom"); err != nil {
			return fmt.Errorf("error: invalid zoom value")
		}

		if x, err = c.ParamsInt("x"); err != nil {
			return fmt.Errorf("error: invalid x value")
		}

		if y, err = c.ParamsInt("y"); err != nil {
			return fmt.Errorf("error: invalid y value")
		}

		var layer model.Source

		for _, l := range app.layers {
			if l.GetKey() == name {
				layer = l
				break
			}
		}

		if layer == nil {
			return c.Status(fiber.StatusNotFound).SendString(fmt.Sprintf("layer %s is not found", name))
		}

		data, err := layer.GetTile(c.Context(), zoom, x, y)

		if err != nil {
			app.logger.Error("error getting tile", "error", err)
			return err
		}

		if data != nil {
			c.Set("Content-Type", layer.GetContentType())
			_, err := c.Write(data)
			if err != nil {
				app.logger.Error("error writing response", "error", err)
			}

			return err
		}

		return c.Status(fiber.StatusNotFound).SendString("not found")
	}
}
