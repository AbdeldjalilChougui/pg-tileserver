package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const geometryColumnsSQL = `
SELECT f_table_schema, f_table_name, f_geometry_column, srid, type
FROM geometry_columns
WHERE f_table_schema NOT IN ('pg_catalog', 'information_schema', 'topology', 'tiger', 'tiger_data')
ORDER BY 1, 2, 3`

const columnsSQL = `
SELECT a.attname, t.typname
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_type t ON t.oid = a.atttypid
WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`

// Discover lists every table registered in PostGIS's geometry_columns view,
// with its exportable attribute columns and WGS84 bounds.
func Discover(ctx context.Context, db *pgxpool.Pool, minZoom, maxZoom int) ([]Layer, error) {
	rows, err := db.Query(ctx, geometryColumnsSQL)
	if err != nil {
		return nil, fmt.Errorf("listing geometry_columns: %w", err)
	}
	type gc struct {
		schema, table, col, typ string
		srid                    int
	}
	var gcs []gc
	perTable := map[string]int{}
	for rows.Next() {
		var g gc
		if err := rows.Scan(&g.schema, &g.table, &g.col, &g.srid, &g.typ); err != nil {
			rows.Close()
			return nil, err
		}
		gcs = append(gcs, g)
		perTable[g.schema+"."+g.table]++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var layers []Layer
	for _, g := range gcs {
		if g.srid <= 0 {
			continue // unknown SRID: cannot be reprojected to Web Mercator
		}
		l := Layer{
			ID:         LayerID(g.schema, g.table, g.col, perTable[g.schema+"."+g.table] > 1),
			Schema:     g.schema,
			Table:      g.table,
			GeomColumn: g.col,
			GeomType:   g.typ,
			SRID:       g.srid,
			MinZoom:    minZoom,
			MaxZoom:    maxZoom,
		}
		crows, err := db.Query(ctx, columnsSQL, g.schema, g.table)
		if err != nil {
			return nil, err
		}
		for crows.Next() {
			var c Column
			if err := crows.Scan(&c.Name, &c.Type); err != nil {
				crows.Close()
				return nil, err
			}
			if c.Name != g.col && IsSupportedType(c.Type) {
				l.Columns = append(l.Columns, c)
			}
		}
		crows.Close()
		l.IDColumn = PickIDColumn(l.Columns)

		var w, s, e, n *float64
		q := fmt.Sprintf(`SELECT ST_XMin(b), ST_YMin(b), ST_XMax(b), ST_YMax(b) FROM (
  SELECT ST_Transform(ST_SetSRID(ST_Extent(%s)::geometry, %d), 4326) AS b FROM %s.%s) AS x`,
			QuoteIdent(g.col), g.srid, QuoteIdent(g.schema), QuoteIdent(g.table))
		if err := db.QueryRow(ctx, q).Scan(&w, &s, &e, &n); err != nil {
			return nil, fmt.Errorf("extent of %s: %w", l.ID, err)
		}
		if w != nil {
			l.Bounds = []float64{*w, *s, *e, *n}
		}
		layers = append(layers, l)
	}
	SortLayers(layers)
	return layers, nil
}
