# pg-tileserver

[![Go Reference](https://pkg.go.dev/badge/github.com/AbdeldjalilChougui/pg-tileserver.svg)](https://pkg.go.dev/github.com/AbdeldjalilChougui/pg-tileserver)

A small vector tile server in Go that turns PostGIS tables into Mapbox Vector
Tiles with `ST_AsMVT` / `ST_TileEnvelope`. It discovers layers on its own,
serves TileJSON, keeps an in-memory LRU cache and ships a MapLibre preview
page. It is a learning project in the spirit of `pg_tileserv` and Martin.

## Install

Install the binary with Go (the preview page is embedded):

```sh
go install github.com/AbdeldjalilChougui/pg-tileserver/cmd/pg-tileserver@latest
```

## Quick start

```sh
docker compose up -d --wait db        # PostGIS 16 + seed data on 127.0.0.1:55433
go run ./cmd/pg-tileserver            # http://localhost:7800/
docker compose down -v                # when done
```

Open <http://localhost:7800/> for the preview, which shows every discovered
layer over an OSM basemap. Click a feature to see its attributes.

| Endpoint | |
|---|---|
| `GET /layers` | discovered layers with columns, SRID, bounds and TileJSON URL |
| `GET /tiles/{layer}.json` | TileJSON 3.0.0 |
| `GET /tiles/{layer}/{z}/{x}/{y}.pbf` | MVT tile (`.mvt` also accepted) |
| `GET /stats` | cache counters |
| `GET /health` | liveness |

Flags (or env): `-db` (`DATABASE_URL`), `-addr` (`ADDR`, default `:7800`), `-base-url` (`BASE_URL`),
`-minzoom`, `-maxzoom` (16), `-cache-mb` (64, 0 disables), `-max-age` (1h), `-max-features` (50000), `-v`.

## How it works

1. **Discovery.** At startup the server reads PostGIS's `geometry_columns`
   view. For each geometry column it keeps the attribute columns of
   MVT-friendly types (text, integers, floats, numeric, bool), picks an integer
   `id`/`gid`/`fid`/`ogc_fid` as the feature id, and computes WGS84 bounds with
   `ST_Extent`. Tables outside `public` are named `schema.table`. Tables with
   several geometry columns get `table.column`. Rows with SRID 0 are skipped.
2. **Tile SQL.** This is generated once per layer
   (`internal/catalog/layer.go`):
   ```sql
   SELECT ST_AsMVT(mvt.*, 'cities', 4096, '__geom', 'id') FROM (
     SELECT ST_AsMVTGeom(ST_Transform(t."geom", 3857), ST_TileEnvelope($1,$2,$3), 4096, 64, true) AS __geom,
            t."id", t."name", ...
     FROM "public"."cities" AS t
     WHERE t."geom" && ST_Transform(ST_TileEnvelope($1,$2,$3, margin => 0.015625), 4326)
     LIMIT 50000
   ) AS mvt WHERE mvt.__geom IS NOT NULL
   ```
   The bbox filter transforms the (buffered) tile envelope into the table's
   SRID, not the other way round, so the GiST index on the native column is
   used. Tables already in 3857 skip both transforms. Identifiers are quoted,
   and z/x/y are bind parameters.
3. **HTTP.** The server validates z/x/y and checks the layer's zoom range. It
   looks up the LRU cache (bounded by bytes, keyed `layer/z/x/y`), then
   queries with a timeout. Empty tiles return `204 No Content`, which MapLibre
   treats as empty. Responses carry `Content-Type:
   application/vnd.mapbox-vector-tile`, `Cache-Control`, `X-Cache: HIT|MISS`
   and CORS `*`. TileJSON URLs come from `-base-url` or the request host, and
   `X-Forwarded-Proto` is honoured.

```
cmd/pg-tileserver   flags, pool, wiring, graceful shutdown
internal/tile       XYZ math (parse, bounds, lon/lat -> tile)
internal/catalog    discovery, tile SQL builder, TileJSON
internal/cache      thread-safe LRU bounded by entries and bytes
internal/server     HTTP handlers + PostGIS source
web/                embedded MapLibre preview
db/init             seed SQL run by the postgis container
integration/        end-to-end tests against docker-compose (build tag)
```

## Tests

```sh
go test ./...                                        # unit tests, no database
docker compose up -d --wait db
go test -count=1 -tags integration ./integration/    # discovery, real tiles, cache, TileJSON
docker compose down -v
```

The unit tests cover tile math, the LRU, the SQL builder (quoting,
3857-vs-4326 paths, limits, id column), TileJSON, and every HTTP path against
a fake source: caching, 204s, 400/404/500/504 and base URLs. The integration
test checks that the three seed layers (points in 4326, lines in 4326, a
hexagon grid in 3857) are discovered with the right columns and bounds, and
that real tiles contain the layer names and attributes.

## Status and limitations

- Layers are discovered once at startup. Restart to pick up new tables.
- There is no per-layer config (zoom ranges, column filters, simplification) and
  no function sources or query parameters.
- Bounds use `ST_Extent` over the whole table, which can be slow on very large tables.
- Tiles are not gzip-compressed by the server. Put a reverse proxy in front for that.
- The cache is in-process and lost on restart. Concurrent misses for the same tile
  are not de-duplicated.
- The preview page loads MapLibre from unpkg and OSM raster tiles from tile.openstreetmap.org.

## Data and license

Code: MIT, see [LICENSE](LICENSE). The seed data in `db/init/01-seed.sql` is
hand-typed approximate city coordinates, made-up straight "corridors" and a
generated hexagon grid. It is not an extract of any dataset. Basemap ©
OpenStreetMap contributors (ODbL).
