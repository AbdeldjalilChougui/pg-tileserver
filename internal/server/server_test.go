package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/AbdeldjalilChougui/pg-tileserver/internal/cache"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/catalog"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/tile"
)

type fakeSource struct {
	calls atomic.Int32
	err   error
}

func (f *fakeSource) Layers() []catalog.Layer {
	return []catalog.Layer{
		{ID: "cities", MinZoom: 0, MaxZoom: 14, Bounds: []float64{-8, 19, 12, 37}, Columns: []catalog.Column{{Name: "name", Type: "text"}}},
		{ID: "osm.roads", MinZoom: 5, MaxZoom: 14},
	}
}

func (f *fakeSource) Tile(ctx context.Context, l catalog.Layer, t tile.Tile) ([]byte, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if t.X%2 == 1 {
		return nil, nil // empty tile
	}
	return []byte("mvt:" + l.ID + "/" + t.String()), nil
}

func newTest(src Source, c *cache.LRU) *Server {
	static := fstest.MapFS{"index.html": {Data: []byte("<html>preview</html>")}}
	return New(src, c, Config{CacheMaxAge: time.Hour}, static, nil)
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://tiles.test"+path, nil))
	return rec
}

func TestTileServedAndCached(t *testing.T) {
	src := &fakeSource{}
	s := newTest(src, cache.New(100, 0))
	rec := get(s, "/tiles/cities/3/4/3.pbf")
	if rec.Code != 200 || rec.Body.String() != "mvt:cities/3/4/3" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if h.Get("Content-Type") != MVTContentType || h.Get("X-Cache") != "MISS" ||
		h.Get("Cache-Control") != "public, max-age=3600" || h.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("headers: %v", h)
	}
	rec = get(s, "/tiles/cities/3/4/3.pbf")
	if rec.Header().Get("X-Cache") != "HIT" || src.calls.Load() != 1 {
		t.Fatalf("expected cache hit, calls=%d", src.calls.Load())
	}
}

func TestEmptyTileIs204(t *testing.T) {
	rec := get(newTest(&fakeSource{}, nil), "/tiles/cities/3/5/3.pbf")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatal(rec.Code)
	}
}

func TestOutOfZoomRangeIs204WithoutQuery(t *testing.T) {
	src := &fakeSource{}
	rec := get(newTest(src, nil), "/tiles/osm.roads/2/0/0.pbf")
	if rec.Code != http.StatusNoContent || src.calls.Load() != 0 {
		t.Fatal(rec.Code, src.calls.Load())
	}
}

func TestBadRequests(t *testing.T) {
	s := newTest(&fakeSource{}, nil)
	for path, want := range map[string]int{
		"/tiles/nope/0/0/0.pbf":   404,
		"/tiles/cities/1/2/0.pbf": 400, // x out of range
		"/tiles/cities/a/0/0.pbf": 400,
		"/tiles/cities/0/0/0.png": 400,
		"/tiles/nope.json":        404,
		"/tiles/cities":           404,
	} {
		if rec := get(s, path); rec.Code != want {
			t.Errorf("%s: got %d want %d", path, rec.Code, want)
		}
	}
}

func TestSourceErrors(t *testing.T) {
	s := newTest(&fakeSource{err: errors.New("boom")}, nil)
	if rec := get(s, "/tiles/cities/0/0/0.pbf"); rec.Code != 500 {
		t.Fatal(rec.Code)
	}
	s = newTest(&fakeSource{err: context.DeadlineExceeded}, nil)
	if rec := get(s, "/tiles/cities/0/0/0.pbf"); rec.Code != 504 {
		t.Fatal(rec.Code)
	}
}

func TestTileJSONAndLayers(t *testing.T) {
	s := newTest(&fakeSource{}, nil)
	rec := get(s, "/tiles/cities.json")
	var tj catalog.TileJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &tj); err != nil {
		t.Fatal(err)
	}
	if tj.Tiles[0] != "http://tiles.test/tiles/cities/{z}/{x}/{y}.pbf" || tj.VectorLayers[0].ID != "cities" {
		t.Fatal(tj)
	}
	rec = get(s, "/tiles/osm.roads.json")
	if rec.Code != 200 {
		t.Fatal("dotted layer ids must work", rec.Code)
	}
	rec = get(s, "/layers")
	var ls []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ls)
	if len(ls) != 2 || ls[0]["tilejson"] != "http://tiles.test/tiles/cities.json" {
		t.Fatal(ls)
	}
}

func TestForwardedProtoAndBaseURL(t *testing.T) {
	s := newTest(&fakeSource{}, nil)
	req := httptest.NewRequest("GET", "http://tiles.test/tiles/cities.json", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var tj catalog.TileJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &tj)
	if tj.Tiles[0][:8] != "https://" {
		t.Fatal(tj.Tiles[0])
	}
	s2 := New(&fakeSource{}, nil, Config{BaseURL: "https://maps.example.org/"}, nil, nil)
	rec = get(s2, "/tiles/cities.json")
	_ = json.Unmarshal(rec.Body.Bytes(), &tj)
	if tj.Tiles[0] != "https://maps.example.org/tiles/cities/{z}/{x}/{y}.pbf" {
		t.Fatal(tj.Tiles[0])
	}
}

func TestPreviewAndHealth(t *testing.T) {
	s := newTest(&fakeSource{}, cache.New(10, 0))
	if rec := get(s, "/"); rec.Code != 200 || rec.Body.String() != "<html>preview</html>" {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := get(s, "/health"); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if rec := get(s, "/stats"); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
}
