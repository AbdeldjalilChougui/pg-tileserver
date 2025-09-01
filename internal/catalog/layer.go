// Package catalog discovers PostGIS geometry tables and builds the SQL that
// turns one of them into a Mapbox Vector Tile.
package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AbdeldjalilChougui/pg-tileserver/internal/tile"
)

// Column is an attribute exported into the tile.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"` // PostgreSQL type name, e.g. "text", "int4"
}

// Layer is one geometry column of one table.
type Layer struct {
	// ID is the public name: "table" for the public schema, "schema.table"
	// otherwise. A table with several geometry columns gets "table.column".
	ID         string    `json:"id"`
	Schema     string    `json:"schema"`
	Table      string    `json:"table"`
	GeomColumn string    `json:"geometry_column"`
	GeomType   string    `json:"geometry_type"`
	SRID       int       `json:"srid"`
	IDColumn   string    `json:"id_column,omitempty"`
	Columns    []Column  `json:"columns"`
	Bounds     []float64 `json:"bounds"` // WGS84 [w, s, e, n]
	MinZoom    int       `json:"minzoom"`
	MaxZoom    int       `json:"maxzoom"`
}

// TileOptions are the knobs of the MVT query.
type TileOptions struct {
	Extent   int // tile coordinate space, 4096 by default
	Buffer   int // pixels of geometry kept outside the tile, 64 by default
	MaxFeats int // LIMIT per tile, 0 = no limit
}

// DefaultTileOptions mirrors what most MVT servers use.
var DefaultTileOptions = TileOptions{Extent: 4096, Buffer: 64, MaxFeats: 50000}

// QuoteIdent quotes an SQL identifier ("a""b" style).
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QuoteLiteral quotes a string literal ('it”s' style).
func QuoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

// supportedTypes are the column types MVT attributes can carry directly.
var supportedTypes = map[string]bool{
	"text": true, "varchar": true, "bpchar": true, "name": true,
	"int2": true, "int4": true, "int8": true,
	"float4": true, "float8": true, "numeric": true,
	"bool": true,
}

// IsSupportedType reports whether a column of this type is exported.
func IsSupportedType(t string) bool { return supportedTypes[t] }

// isIntegerType is used to pick a feature id column.
func isIntegerType(t string) bool { return t == "int2" || t == "int4" || t == "int8" }

// PickIDColumn chooses an integer "id", "gid", "fid" or "ogc_fid" column.
func PickIDColumn(cols []Column) string {
	for _, want := range []string{"id", "gid", "fid", "ogc_fid"} {
		for _, c := range cols {
			if c.Name == want && isIntegerType(c.Type) {
				return c.Name
			}
		}
	}
	return ""
}

// LayerID returns the public name for a table geometry column.
func LayerID(schema, table, geomCol string, multiGeom bool) string {
	id := table
	if schema != "public" {
		id = schema + "." + table
	}
	if multiGeom {
		id += "." + geomCol
	}
	return id
}

// SortLayers orders layers by ID for stable listings.
func SortLayers(ls []Layer) {
	sort.Slice(ls, func(i, j int) bool { return ls[i].ID < ls[j].ID })
}

// TileSQL builds the query for one tile. Parameters are $1=z, $2=x, $3=y.
//
// The geometry is filtered with the index-friendly `&&` operator against the
// tile envelope, expanded by the buffer and transformed into the table's SRID
// (so the spatial index on the native column is used), then clipped and
// quantised by ST_AsMVTGeom in EPSG:3857.
func TileSQL(l Layer, o TileOptions) string {
	if o.Extent <= 0 {
		o.Extent = 4096
	}
	if o.Buffer < 0 {
		o.Buffer = 0
	}
	g := "t." + QuoteIdent(l.GeomColumn)
	geom3857 := g
	if l.SRID != 3857 {
		geom3857 = fmt.Sprintf("ST_Transform(%s, 3857)", g)
	}
	margin := float64(o.Buffer) / float64(o.Extent)
	env := fmt.Sprintf("ST_TileEnvelope($1, $2, $3, margin => %g)", margin)
	if l.SRID != 3857 {
		env = fmt.Sprintf("ST_Transform(%s, %d)", env, l.SRID)
	}

	cols := make([]string, 0, len(l.Columns))
	for _, c := range l.Columns {
		cols = append(cols, "t."+QuoteIdent(c.Name))
	}
	sel := fmt.Sprintf("ST_AsMVTGeom(%s, ST_TileEnvelope($1, $2, $3), %d, %d, true) AS __geom",
		geom3857, o.Extent, o.Buffer)
	if len(cols) > 0 {
		sel += ", " + strings.Join(cols, ", ")
	}
	limit := ""
	if o.MaxFeats > 0 {
		limit = fmt.Sprintf(" LIMIT %d", o.MaxFeats)
	}
	idArg := ""
	if l.IDColumn != "" {
		idArg = ", " + QuoteLiteral(l.IDColumn)
	}
	return fmt.Sprintf(`SELECT ST_AsMVT(mvt.*, %s, %d, '__geom'%s) FROM (
  SELECT %s
  FROM %s.%s AS t
  WHERE %s && %s%s
) AS mvt WHERE mvt.__geom IS NOT NULL`,
		QuoteLiteral(l.ID), o.Extent, idArg,
		sel,
		QuoteIdent(l.Schema), QuoteIdent(l.Table),
		g, env, limit)
}

// TileJSON is the subset of the TileJSON 3.0.0 spec the server emits.
type TileJSON struct {
	TileJSON     string        `json:"tilejson"`
	Name         string        `json:"name"`
	Scheme       string        `json:"scheme"`
	Tiles        []string      `json:"tiles"`
	MinZoom      int           `json:"minzoom"`
	MaxZoom      int           `json:"maxzoom"`
	Bounds       []float64     `json:"bounds,omitempty"`
	Center       []float64     `json:"center,omitempty"`
	VectorLayers []VectorLayer `json:"vector_layers"`
}

// VectorLayer describes one source-layer inside the tiles.
type VectorLayer struct {
	ID      string            `json:"id"`
	Fields  map[string]string `json:"fields"`
	MinZoom int               `json:"minzoom"`
	MaxZoom int               `json:"maxzoom"`
}

// FieldType maps a PostgreSQL type to a TileJSON field type.
func FieldType(pgType string) string {
	switch pgType {
	case "int2", "int4", "int8", "float4", "float8", "numeric":
		return "Number"
	case "bool":
		return "Boolean"
	default:
		return "String"
	}
}

// BuildTileJSON describes a layer; baseURL is like "http://host:port".
func BuildTileJSON(l Layer, baseURL string) TileJSON {
	fields := map[string]string{}
	for _, c := range l.Columns {
		fields[c.Name] = FieldType(c.Type)
	}
	tj := TileJSON{
		TileJSON: "3.0.0",
		Name:     l.ID,
		Scheme:   "xyz",
		Tiles:    []string{fmt.Sprintf("%s/tiles/%s/{z}/{x}/{y}.pbf", strings.TrimRight(baseURL, "/"), l.ID)},
		MinZoom:  l.MinZoom,
		MaxZoom:  l.MaxZoom,
		VectorLayers: []VectorLayer{{
			ID: l.ID, Fields: fields, MinZoom: l.MinZoom, MaxZoom: l.MaxZoom,
		}},
	}
	if len(l.Bounds) == 4 {
		b := l.Bounds
		tj.Bounds = b
		z := FitZoom(b)
		tj.Center = []float64{(b[0] + b[2]) / 2, (b[1] + b[3]) / 2, float64(max(l.MinZoom, min(z, l.MaxZoom)))}
	}
	return tj
}

// FitZoom is the largest zoom at which the bounds fit within roughly one tile.
func FitZoom(b []float64) int {
	if len(b) != 4 {
		return 0
	}
	for z := 0; z <= 20; z++ {
		a := tile.FromLonLat(b[0], b[3], z)
		c := tile.FromLonLat(b[2], b[1], z)
		if c.X-a.X > 1 || c.Y-a.Y > 1 {
			return max(0, z-1)
		}
	}
	return 20
}
