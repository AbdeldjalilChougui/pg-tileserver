package catalog

import (
	"strings"
	"testing"
)

func TestQuoting(t *testing.T) {
	if got := QuoteIdent(`we"ird`); got != `"we""ird"` {
		t.Fatal(got)
	}
	if got := QuoteLiteral(`it's`); got != `'it''s'` {
		t.Fatal(got)
	}
}

func TestLayerID(t *testing.T) {
	cases := []struct {
		schema, table, col string
		multi              bool
		want               string
	}{
		{"public", "cities", "geom", false, "cities"},
		{"osm", "roads", "geom", false, "osm.roads"},
		{"public", "parcels", "centroid", true, "parcels.centroid"},
	}
	for _, c := range cases {
		if got := LayerID(c.schema, c.table, c.col, c.multi); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}

func TestPickIDColumn(t *testing.T) {
	cols := []Column{{"name", "text"}, {"id", "text"}, {"gid", "int4"}}
	if got := PickIDColumn(cols); got != "gid" {
		t.Fatalf("text id must be skipped, got %q", got)
	}
	if got := PickIDColumn([]Column{{"name", "text"}}); got != "" {
		t.Fatal(got)
	}
}

func TestTileSQL4326(t *testing.T) {
	l := Layer{ID: "cities", Schema: "public", Table: "cities", GeomColumn: "geom", SRID: 4326,
		IDColumn: "id", Columns: []Column{{"id", "int4"}, {"name", "text"}}}
	sql := TileSQL(l, DefaultTileOptions)
	for _, want := range []string{
		`ST_AsMVT(mvt.*, 'cities', 4096, '__geom', 'id')`,
		`ST_AsMVTGeom(ST_Transform(t."geom", 3857), ST_TileEnvelope($1, $2, $3), 4096, 64, true)`,
		`t."id", t."name"`,
		`FROM "public"."cities" AS t`,
		`t."geom" && ST_Transform(ST_TileEnvelope($1, $2, $3, margin => 0.015625), 4326)`,
		`LIMIT 50000`,
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in\n%s", want, sql)
		}
	}
}

func TestTileSQL3857NoTransform(t *testing.T) {
	l := Layer{ID: "osm.grid", Schema: "osm", Table: "grid", GeomColumn: "the geom", SRID: 3857}
	sql := TileSQL(l, TileOptions{Extent: 512, Buffer: 0})
	if strings.Contains(sql, "ST_Transform") {
		t.Errorf("3857 layers must not be transformed:\n%s", sql)
	}
	if strings.Contains(sql, "LIMIT") {
		t.Error("MaxFeats=0 means no limit")
	}
	if !strings.Contains(sql, `t."the geom" && ST_TileEnvelope($1, $2, $3, margin => 0)`) {
		t.Error(sql)
	}
	if !strings.Contains(sql, `ST_AsMVT(mvt.*, 'osm.grid', 512, '__geom')`) {
		t.Error(sql)
	}
}

func TestTileJSON(t *testing.T) {
	l := Layer{ID: "cities", MinZoom: 0, MaxZoom: 14, Bounds: []float64{-8.7, 19, 12, 37.1},
		Columns: []Column{{"name", "text"}, {"pop", "int8"}, {"capital", "bool"}}}
	tj := BuildTileJSON(l, "http://localhost:7800/")
	if tj.Tiles[0] != "http://localhost:7800/tiles/cities/{z}/{x}/{y}.pbf" {
		t.Fatal(tj.Tiles[0])
	}
	f := tj.VectorLayers[0].Fields
	if f["name"] != "String" || f["pop"] != "Number" || f["capital"] != "Boolean" {
		t.Fatal(f)
	}
	if tj.TileJSON != "3.0.0" || len(tj.Center) != 3 {
		t.Fatal(tj)
	}
	if z := tj.Center[2]; z < 2 || z > 5 {
		t.Fatalf("center zoom for Algeria should be ~3-4, got %v", z)
	}
}

func TestFieldTypeDefault(t *testing.T) {
	if FieldType("varchar") != "String" || FieldType("numeric") != "Number" {
		t.Fatal()
	}
}
