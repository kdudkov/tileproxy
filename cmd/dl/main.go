package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/kdudkov/tileproxy/pkg/model"
	"github.com/schollz/progressbar/v3"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

func loadSource(config, cacheDir, key string) (model.Source, error) {
	data, err := os.ReadFile(config)
	if err != nil {
		return nil, err
	}
	var descriptions []*model.LayerDescription
	if err := yaml.Unmarshal(data, &descriptions); err != nil {
		return nil, err
	}
	for _, d := range descriptions {
		if d != nil && d.Key == key && key != "" {
			return model.NewProxy(d, slog.Default(), cacheDir), nil
		}
	}
	return nil, fmt.Errorf("unknown layer %q in %s", key, config)
}

func printPlan(out io.Writer, source model.Source, plans []zoomPlan, tileKiB float64) (int64, error) {
	fmt.Fprintf(out, "Layer: %s (%s)\n", source.GetName(), source.GetKey())
	fmt.Fprintf(out, "Estimate: %.1f KiB/tile + 10%% SQLite overhead; no network sampling.\n", tileKiB)
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "Zoom\tTiles\tEstimated MiB")
	var total int64
	for _, p := range plans {
		total += p.count
		fmt.Fprintf(table, "%d\t%d\t%.2f\n", p.z, p.count, float64(p.count)*tileKiB*1.1/1024)
	}
	// Fixed allowance for empty SQLite pages and metadata, in addition to per-tile overhead.
	fmt.Fprintf(table, "Total\t%d\t%.2f\n", total, float64(total)*tileKiB*1.1/1024+16.0/1024)
	return total, table.Flush()
}

type tileJob struct {
	tile     tile
	url      string
	attempts int
}

type tileResult struct {
	job  tileJob
	data []byte
	err  error
}

func download(ctx context.Context, source model.Source, p contour, plans []zoomPlan, filename, title string, workers, retries int, progress io.Writer) error {
	if retries < 0 {
		return fmt.Errorf("retries must be nonnegative")
	}
	if workers < 1 {
		return fmt.Errorf("workers must be positive")
	}
	if _, err := os.Lstat(filename); err == nil {
		return fmt.Errorf("output already exists: %s", filename)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	format := ""
	switch source.GetContentType() {
	case "image/png":
		format = "png"
	case "image/jpeg":
		format = "jpg"
	case "image/webp":
		format = "webp"
	default:
		return fmt.Errorf("unsupported tile content type %q", source.GetContentType())
	}
	tmp, err := os.CreateTemp(filepath.Dir(filename), ".tileproxy-*.mbtiles")
	if err != nil {
		return err
	}
	tempName := tmp.Name()
	defer os.Remove(tempName)
	if err := tmp.Close(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", tempName)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE tiles (zoom_level INTEGER NOT NULL, tile_column INTEGER NOT NULL, tile_row INTEGER NOT NULL, tile_data BLOB NOT NULL, PRIMARY KEY (zoom_level,tile_column,tile_row)); CREATE TABLE metadata (name TEXT PRIMARY KEY, value TEXT);`); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO tiles VALUES (?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	// ponytail: keep one job per selected tile; use a disk queue for very large regions.
	queue := make([]tileJob, 0)
	proxy, _ := source.(*model.Proxy)
	for _, plan := range plans {
		if err := p.walk(ctx, plan, func(t tile) error {
			job := tileJob{tile: t}
			if proxy != nil {
				y := t.y
				if proxy.IsTms() {
					y = (1 << t.z) - 1 - y
				}
				job.url = proxy.GetUrl(t.z, t.x, y)
			}
			queue = append(queue, job)
			return nil
		}); err != nil {
			return err
		}
	}
	total := int64(len(queue))
	ctx, cancel := context.WithCancel(ctx)
	jobs := make(chan tileJob)
	results := make(chan tileResult, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				t := job.tile
				var data []byte
				var err error
				if proxy != nil {
					_, data, err = proxy.GetTileFromURL(ctx, t.z, t.x, t.y, job.url)
				} else {
					_, data, err = source.GetTile(ctx, t.z, t.x, t.y)
				}
				if err == nil && len(data) == 0 {
					err = fmt.Errorf("empty tile")
				}
				select {
				case results <- tileResult{job, data, err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	defer func() { cancel(); close(jobs); wg.Wait() }()
	bar := progressbar.NewOptions64(total, progressbar.OptionSetWriter(progress), progressbar.OptionSetDescription("tiles saved"))
	var done int64
	inFlight := 0
	for len(queue) > 0 || inFlight > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		var send chan tileJob
		var next tileJob
		if len(queue) > 0 {
			send = jobs
			next = queue[0]
			next.attempts++
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case send <- next:
			queue[0] = tileJob{}
			queue = queue[1:]
			inFlight++
		case result := <-results:
			inFlight--
			job := result.job
			t := job.tile
			if result.err != nil {
				if job.attempts > retries {
					return fmt.Errorf("tile %d/%d/%d failed after %d attempts: %w", t.z, t.x, t.y, job.attempts, result.err)
				}
				queue = append(queue, job)
				fmt.Fprintf(progress, "\nRetry queued: %d/%d/%d (attempt %d failed): %v\n", t.z, t.x, t.y, job.attempts, result.err)
				continue
			}
			if _, err := stmt.ExecContext(ctx, t.z, t.x, (1<<t.z)-1-t.y, result.data); err != nil {
				return err
			}
			done++
			_ = bar.Add(1)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if done != total || done == 0 {
		return fmt.Errorf("saved %d of %d tiles", done, total)
	}
	minZ, maxZ := 30, 0
	for _, plan := range plans {
		if plan.count > 0 {
			minZ = min(minZ, plan.z)
			maxZ = max(maxZ, plan.z)
		}
	}
	metadata := map[string]string{"name": title, "format": format, "type": "baselayer", "version": "1.0", "scheme": "tms", "minzoom": strconv.Itoa(minZ), "maxzoom": strconv.Itoa(maxZ)}
	for key, value := range metadata {
		if _, err := tx.ExecContext(ctx, "INSERT INTO metadata VALUES (?,?)", key, value); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	// Publish only a complete database, without overwriting a file created during download.
	if err := os.Link(tempName, filename); err != nil {
		return err
	}
	return nil
}

func run(ctx context.Context, args []string, out, stderr io.Writer) error {
	flags := flag.NewFlagSet("dl", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cache := flags.String("path", "data", "tile cache directory")
	config := flags.String("layers", "layers.yml", "layer configuration file")
	layer := flags.String("layer", "", "layer key from layers.yml (required)")
	output := flags.String("map_name", "", "output MBTiles file (must not exist)")
	title := flags.String("title", "", "MBTiles title")
	workers := flags.Int("n", 2, "parallel downloads (positive)")
	retries := flags.Int("retries", 3, "retries per tile after the first attempt (nonnegative)")
	minZ := flags.Int("minZ", -1, "minimum zoom, inclusive (required)")
	maxZ := flags.Int("maxZ", -1, "maximum zoom, inclusive (required)")
	tileKiB := flags.Float64("tile-size-kib", 32, "average tile size for estimate, in KiB")
	dryRun := flags.Bool("dry-run", false, "print statistics without downloading or creating MBTiles")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: dl -minZ 10 -maxZ 16 -layer google_h [options] contour.geojson")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("expected one contour.geojson filename; use -h for help")
	}
	if *minZ < 0 || *maxZ < *minZ || *maxZ > 30 {
		return fmt.Errorf("required zoom range: 0 <= minZ <= maxZ <= 30")
	}
	if *retries < 0 {
		return fmt.Errorf("-retries must be nonnegative")
	}
	if *workers < 1 {
		return fmt.Errorf("-n must be positive")
	}
	if *tileKiB <= 0 || math.IsNaN(*tileKiB) || math.IsInf(*tileKiB, 0) {
		return fmt.Errorf("-tile-size-kib must be finite and positive")
	}
	source, err := loadSource(*config, *cache, *layer)
	if err != nil {
		return err
	}
	if *minZ < source.GetMinZoom() || *maxZ > source.GetMaxZoom() {
		return fmt.Errorf("layer %s supports zoom %d..%d, requested %d..%d", *layer, source.GetMinZoom(), source.GetMaxZoom(), *minZ, *maxZ)
	}
	p, err := readContour(flags.Arg(0))
	if err != nil {
		return err
	}
	plans, err := p.plan(ctx, *minZ, *maxZ)
	if err != nil {
		return err
	}
	total, err := printPlan(out, source, plans, *tileKiB)
	if err != nil {
		return err
	}
	if *dryRun {
		return nil
	}
	if total == 0 {
		return fmt.Errorf("no tiles have a corner inside or on the contour; try a higher zoom")
	}
	if *output == "" {
		base := filepath.Base(flags.Arg(0))
		*output = strings.TrimSuffix(base, filepath.Ext(base)) + "_" + source.GetKey() + ".mbtiles"
	}
	if !strings.HasSuffix(*output, ".mbtiles") {
		*output += ".mbtiles"
	}
	if *title == "" {
		*title = source.GetName() + " " + filepath.Base(flags.Arg(0))
	}
	if err := download(ctx, source, p, plans, *output, *title, *workers, *retries, stderr); err != nil {
		return err
	}
	info, err := os.Stat(*output)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved %s: %d tiles, %.2f MiB\n", *output, total, float64(info.Size())/(1024*1024))
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
