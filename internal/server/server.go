// Package server exposes layers as XYZ vector tiles and TileJSON over HTTP.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/AbdeldjalilChougui/pg-tileserver/internal/cache"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/catalog"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/tile"
)

// Source produces tiles for known layers. The PostGIS implementation lives
// in pgsource.go; tests use a fake.
type Source interface {
	Layers() []catalog.Layer
	Tile(ctx context.Context, l catalog.Layer, t tile.Tile) ([]byte, error)
}

// Config tunes the HTTP layer.
type Config struct {
	BaseURL      string        // public URL used in TileJSON; empty = derive from the request
	CacheMaxAge  time.Duration // Cache-Control max-age for tiles
	TileTimeout  time.Duration // per-tile query timeout
	AllowOrigins string        // Access-Control-Allow-Origin value, "*" by default
}

// Server is an http.Handler.
type Server struct {
	src   Source
	cache *cache.LRU
	cfg   Config
	mux   *http.ServeMux
	log   *slog.Logger
}

// MVTContentType is the registered media type for vector tiles.
const MVTContentType = "application/vnd.mapbox-vector-tile"

// New wires the routes. c may be nil (no cache); static may be nil (no preview).
func New(src Source, c *cache.LRU, cfg Config, static fs.FS, log *slog.Logger) *Server {
	if cfg.TileTimeout == 0 {
		cfg.TileTimeout = 10 * time.Second
	}
	if cfg.AllowOrigins == "" {
		cfg.AllowOrigins = "*"
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{src: src, cache: c, cfg: cfg, mux: http.NewServeMux(), log: log}
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	s.mux.HandleFunc("GET /layers", s.handleLayers)
	s.mux.HandleFunc("GET /stats", s.handleStats)
	s.mux.HandleFunc("GET /tiles/{file}", s.handleTileJSON)
	s.mux.HandleFunc("GET /tiles/{layer}/{z}/{x}/{y}", s.handleTile)
	if static != nil {
		s.mux.Handle("GET /", http.FileServerFS(static))
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", s.cfg.AllowOrigins)
	s.mux.ServeHTTP(w, r)
}

func (s *Server) layer(id string) (catalog.Layer, bool) {
	for _, l := range s.src.Layers() {
		if l.ID == id {
			return l, true
		}
	}
	return catalog.Layer{}, false
}

func (s *Server) baseURL(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return strings.TrimRight(s.cfg.BaseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "http" || p == "https" {
		scheme = p
	}
	return scheme + "://" + r.Host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func (s *Server) handleLayers(w http.ResponseWriter, r *http.Request) {
	type item struct {
		catalog.Layer
		TileJSON string `json:"tilejson"`
	}
	base := s.baseURL(r)
	out := []item{}
	for _, l := range s.src.Layers() {
		out = append(out, item{l, fmt.Sprintf("%s/tiles/%s.json", base, l.ID)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	if s.cache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"cache": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cache": s.cache.Stats()})
}

func (s *Server) handleTileJSON(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	id, ok := strings.CutSuffix(file, ".json")
	if !ok {
		http.NotFound(w, r)
		return
	}
	l, ok := s.layer(id)
	if !ok {
		http.Error(w, "unknown layer", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, catalog.BuildTileJSON(l, s.baseURL(r)))
}

func (s *Server) handleTile(w http.ResponseWriter, r *http.Request) {
	l, ok := s.layer(r.PathValue("layer"))
	if !ok {
		http.Error(w, "unknown layer", http.StatusNotFound)
		return
	}
	t, _, err := tile.Parse(r.PathValue("z"), r.PathValue("x"), r.PathValue("y"), "pbf", "mvt")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if t.Z < l.MinZoom || t.Z > l.MaxZoom {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	key := l.ID + "/" + t.String()
	var data []byte
	hit := false
	if s.cache != nil {
		data, hit = s.cache.Get(key)
	}
	if !hit {
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.TileTimeout)
		defer cancel()
		start := time.Now()
		data, err = s.src.Tile(ctx, l, t)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, context.DeadlineExceeded) {
				status = http.StatusGatewayTimeout
			}
			s.log.Error("tile query failed", "layer", l.ID, "tile", t.String(), "err", err)
			http.Error(w, "tile query failed", status)
			return
		}
		s.log.Debug("tile", "layer", l.ID, "tile", t.String(), "bytes", len(data), "took", time.Since(start))
		if s.cache != nil {
			s.cache.Add(key, data)
		}
	}
	h := w.Header()
	if s.cfg.CacheMaxAge > 0 {
		h.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(s.cfg.CacheMaxAge.Seconds())))
	}
	if hit {
		h.Set("X-Cache", "HIT")
	} else {
		h.Set("X-Cache", "MISS")
	}
	if len(data) == 0 {
		// Nothing in this tile: MapLibre treats 204 as an empty tile.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.Set("Content-Type", MVTContentType)
	h.Set("Content-Length", fmt.Sprint(len(data)))
	_, _ = w.Write(data)
}
