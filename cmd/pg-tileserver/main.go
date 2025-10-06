// Command pg-tileserver serves PostGIS tables as Mapbox Vector Tiles.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AbdeldjalilChougui/pg-tileserver/internal/cache"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/catalog"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/server"
	"github.com/AbdeldjalilChougui/pg-tileserver/web"
	"github.com/jackc/pgx/v5/pgxpool"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	var (
		dsn      = flag.String("db", env("DATABASE_URL", "postgres://tiles:tiles@localhost:55433/tiles"), "PostgreSQL connection string (or DATABASE_URL)")
		addr     = flag.String("addr", env("ADDR", ":7800"), "listen address")
		baseURL  = flag.String("base-url", env("BASE_URL", ""), "public base URL used in TileJSON (default: from request)")
		minZoom  = flag.Int("minzoom", 0, "minimum zoom served")
		maxZoom  = flag.Int("maxzoom", 16, "maximum zoom served")
		cacheMB  = flag.Int("cache-mb", 64, "in-memory tile cache size in MiB (0 disables)")
		maxAge   = flag.Duration("max-age", time.Hour, "Cache-Control max-age for tiles")
		maxFeats = flag.Int("max-features", 50000, "maximum features per tile per layer (0 = unlimited)")
		verbose  = flag.Bool("v", false, "debug logging")
	)
	flag.Parse()
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Error("connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	opts := catalog.DefaultTileOptions
	opts.MaxFeats = *maxFeats
	src := &server.PGSource{DB: pool, Options: opts}
	if err := src.Refresh(ctx, *minZoom, *maxZoom); err != nil {
		log.Error("layer discovery", "err", err)
		os.Exit(1)
	}
	for _, l := range src.Layers() {
		log.Info("layer", "id", l.ID, "type", l.GeomType, "srid", l.SRID, "columns", len(l.Columns))
	}

	var c *cache.LRU
	if *cacheMB > 0 {
		c = cache.New(0, int64(*cacheMB)<<20)
	}
	h := server.New(src, c, server.Config{BaseURL: *baseURL, CacheMaxAge: *maxAge}, web.FS, log)
	srv := &http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	preview := *addr
	if strings.HasPrefix(preview, ":") {
		preview = "localhost" + preview
	}
	log.Info("listening", "addr", *addr, "preview", "http://"+preview+"/")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
