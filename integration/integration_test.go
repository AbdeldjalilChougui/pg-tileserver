//go:build integration

// Package integration runs the real server against the docker-compose
// PostGIS:
//
//	docker compose up -d --wait db
//	go test -tags integration ./integration/
//	docker compose down -v
package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/AbdeldjalilChougui/pg-tileserver/internal/cache"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/catalog"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/server"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setup(t *testing.T) (*httptest.Server, *server.PGSource) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://tiles:tiles@localhost:55433/tiles"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	src := &server.PGSource{DB: pool, Options: catalog.DefaultTileOptions}
	if err := src.Refresh(ctx, 0, 16); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.New(src, cache.New(100, 0), server.Config{}, nil, nil))
	t.Cleanup(ts.Close)
	return ts, src
}

func TestDiscovery(t *testing.T) {
	_, src := setup(t)
	byID := map[string]catalog.Layer{}
	for _, l := range src.Layers() {
		byID[l.ID] = l
	}
	c, ok := byID["cities"]
	if !ok || c.SRID != 4326 || c.GeomType != "POINT" || c.IDColumn != "id" {
		t.Fatalf("cities layer: %+v", c)
	}
	if len(c.Columns) != 6 { // id, name, name_ar, wilaya, population, capital
		t.Fatalf("columns: %+v", c.Columns)
	}
	if b := c.Bounds; len(b) != 4 || b[0] > -8 || b[2] < 8 || b[1] > 23 || b[3] < 36.9 {
		t.Fatalf("bounds: %v", b)
	}
	if h := byID["hexgrid"]; h.SRID != 3857 || h.GeomType != "POLYGON" {
		t.Fatalf("hexgrid: %+v", h)
	}
	if l := byID["corridors"]; l.IDColumn != "gid" {
		t.Fatalf("corridors: %+v", l)
	}
}

func fetch(t *testing.T, url string) (*http.Response, []byte) {
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func TestTiles(t *testing.T) {
	ts, _ := setup(t)
	for _, layer := range []string{"cities", "corridors", "hexgrid"} {
		// z4 tile 8/6 covers northern Algeria.
		res, body := fetch(t, ts.URL+"/tiles/"+layer+"/4/8/6.pbf")
		if res.StatusCode != 200 || len(body) < 20 {
			t.Fatalf("%s: status %d, %d bytes", layer, res.StatusCode, len(body))
		}
		if res.Header.Get("Content-Type") != server.MVTContentType {
			t.Fatal(res.Header)
		}
		// Layer name appears in the protobuf as a length-prefixed string (field 1 of the layer).
		if !containsBytes(body, []byte(layer)) {
			t.Fatalf("%s: layer name not found in tile", layer)
		}
	}
	// Middle of the Pacific: empty.
	res, _ := fetch(t, ts.URL+"/tiles/cities/6/1/30.pbf")
	if res.StatusCode != 204 {
		t.Fatalf("empty tile: %d", res.StatusCode)
	}
	// Second request is served from the cache.
	res, _ = fetch(t, ts.URL+"/tiles/cities/4/8/6.pbf")
	if res.Header.Get("X-Cache") != "HIT" {
		t.Fatal("expected cache hit")
	}
	// Attributes are present.
	_, body := fetch(t, ts.URL+"/tiles/cities/4/8/6.pbf")
	if !containsBytes(body, []byte("Alger")) || !containsBytes(body, []byte("population")) {
		t.Fatal("attributes missing")
	}
}

func TestTileJSON(t *testing.T) {
	ts, _ := setup(t)
	_, body := fetch(t, ts.URL+"/tiles/hexgrid.json")
	var tj catalog.TileJSON
	if err := json.Unmarshal(body, &tj); err != nil {
		t.Fatal(err)
	}
	if tj.VectorLayers[0].Fields["cities"] != "Number" || len(tj.Bounds) != 4 {
		t.Fatalf("%+v", tj)
	}
}

func containsBytes(h, n []byte) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if string(h[i:i+len(n)]) == string(n) {
			return true
		}
	}
	return false
}
