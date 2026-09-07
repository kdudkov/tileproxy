# Tileproxy

A simple and fast TMS proxy/cache and tileserver

It provides:

* [TMS](https://wiki.openstreetmap.org/wiki/TMS)  endpoint to proxy and cache tile server requests, cache layout is
  compatible with [SAS.Planet](https://www.sasgis.org/sasplaneta/)
* TMS endpoint to serve tiles from [mbtiles](https://wiki.openstreetmap.org/wiki/MBTiles) files

example:

```bash
tileserver -addr :8080 -files ./files -cache ./cache
```

Open `/map`, click **Нарисовать полигон**, and place vertices on the map.
Click **Завершить** to close the contour. Invalid edges or edits are rejected;
use **Удалить последнюю точку** to correct a draft. Drag a vertex to move it,
or click the completed polygon to insert a vertex near the closest edge.
**Скачать GeoJSON** downloads `contour.geojson`: one GeoJSON Polygon with a
closed, counterclockwise outer ring in `[longitude, latitude]` order.

Create MBTiles from the downloaded contour (flags must precede the filename):

```bash
go run ./cmd/dl -minZ 10 -maxZ 16 -layer google_h -map_name region.mbtiles contour.geojson
```

The CLI prints tile counts and estimated size for every zoom, then downloads
only tiles with at least one corner inside or on the contour boundary. A tile
that encloses the entire contour, or only crosses its edges, is skipped if none
of its corners qualifies. Zoom limits are inclusive and must be supported by
the selected layer in `layers.yml`.

The estimate uses 32 KiB per tile, plus 10% SQLite overhead and 16 KiB for fixed
pages/metadata. It makes no network requests and may differ substantially from
the actual size. Set `-tile-size-kib 50` to change the assumption, or `-dry-run`
to print statistics without downloading or creating a database.

`-path` sets the tile cache directory (default `data`); `-layers` selects the
configuration file; `-n` sets concurrent downloads (default 2). Existing output
files are never overwritten. Failed tile downloads are appended to the end of the queue with the same URL,
allowing other tiles to proceed. Each tile gets 3 retries after its first attempt
(4 attempts total); change this with `-retries`, or set `-retries 0` to disable
retries. Exhausted retries or database write failures return a nonzero exit code
and discard the incomplete database; downloaded cache entries remain. Ctrl+C
cancels pending downloads and retries.

Input must be a simple GeoJSON Polygon with one closed outer ring, no holes,
and coordinates within Web Mercator bounds. Antimeridian crossings and the
old `z/x/y` tile-list input are not supported.

Run CLI tests with `go test ./cmd/dl`.

Run the contour validation and export tests with Node.js:

```bash
node --test tests/polygon.test.cjs
```
