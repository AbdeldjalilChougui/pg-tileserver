package server

import (
	"context"
	"sync"

	"github.com/AbdeldjalilChougui/pg-tileserver/internal/catalog"
	"github.com/AbdeldjalilChougui/pg-tileserver/internal/tile"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGSource serves tiles straight from PostGIS.
type PGSource struct {
	DB      *pgxpool.Pool
	Options catalog.TileOptions

	mu     sync.RWMutex
	layers []catalog.Layer
	sql    map[string]string
}

// Refresh re-runs layer discovery.
func (p *PGSource) Refresh(ctx context.Context, minZoom, maxZoom int) error {
	ls, err := catalog.Discover(ctx, p.DB, minZoom, maxZoom)
	if err != nil {
		return err
	}
	sql := make(map[string]string, len(ls))
	for _, l := range ls {
		sql[l.ID] = catalog.TileSQL(l, p.Options)
	}
	p.mu.Lock()
	p.layers, p.sql = ls, sql
	p.mu.Unlock()
	return nil
}

func (p *PGSource) Layers() []catalog.Layer {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.layers
}

func (p *PGSource) Tile(ctx context.Context, l catalog.Layer, t tile.Tile) ([]byte, error) {
	p.mu.RLock()
	q := p.sql[l.ID]
	p.mu.RUnlock()
	var data []byte
	err := p.DB.QueryRow(ctx, q, t.Z, t.X, t.Y).Scan(&data)
	return data, err
}
