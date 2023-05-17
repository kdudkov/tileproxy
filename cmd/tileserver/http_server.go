package main

import (
	"embed"
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/redirect"
)

//go:embed static/*
var embedDirStatic embed.FS

func NewHttp(app *App) *fiber.App {
	f := fiber.New()

	f.Use(logger.New(logger.Config{
		Format: "[${ip}]:${port} ${status} - ${locals:username} ${method} ${path} ${queryParams}\n",
	}))

	f.Use(redirect.New(redirect.Config{
		Rules: map[string]string{
			"/": "/static/index.html",
		},
		StatusCode: 302,
	}))

	f.Get("/layers", getLayersHandler(app))
	f.Get("/tiles/:name/:zoom/:x/:y", getTileHandler(app))

	f.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(embedDirStatic),
		PathPrefix: "static",
	}))

	return f
}

func getLayersHandler(app *App) func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		r := make([]map[string]interface{}, 0)
		for key, l := range app.layers {
			ld := make(map[string]interface{})
			ld["url"] = "/tiles/" + key + "/{z}/{x}/{y}"
			ld["minzoom"] = l.GetMinZoom()
			ld["maxzoom"] = l.GetMaxZoom()
			ld["name"] = l.GetName()
			ld["file"] = l.IsFile()
			r = append(r, ld)
		}
		return c.JSON(r)
	}
}

func getTileHandler(app *App) func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		var err error
		var zoom, x, y int

		name := c.Params("name")

		if zoom, err = c.ParamsInt("zoom"); err != nil {
			return fmt.Errorf("error: invalid zoom value")
		}

		if x, err = c.ParamsInt("x"); err != nil {
			return fmt.Errorf("error: invalid x value")
		}

		if y, err = c.ParamsInt("y"); err != nil {
			return fmt.Errorf("error: invalid y value")
		}

		if _, ok := app.layers[name]; !ok {
			return fmt.Errorf("invalid name")
		}

		l, ok := app.layers[name]

		if !ok {
			return c.Status(fiber.StatusNotFound).SendString(fmt.Sprintf("layer %s is not found", name))
		}

		data, err := l.GetTile(c.Context(), zoom, x, y)

		if err != nil {
			fmt.Println(err)
			return err
		}

		if data != nil {
			c.Set("Content-Type", l.GetContentType())
			_, err := c.Write(data)
			return err
		}

		return c.Status(fiber.StatusNotFound).SendString("not found")
	}
}
