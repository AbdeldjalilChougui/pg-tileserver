package tile

import (
	"errors"
	"math"
	"testing"
)

func near(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestValid(t *testing.T) {
	cases := []struct {
		t  Tile
		ok bool
	}{
		{Tile{0, 0, 0}, true},
		{Tile{0, 1, 0}, false},
		{Tile{1, 1, 1}, true},
		{Tile{1, 2, 0}, false},
		{Tile{-1, 0, 0}, false},
		{Tile{3, 7, 7}, true},
		{Tile{3, -1, 0}, false},
		{Tile{MaxZoom + 1, 0, 0}, false},
	}
	for _, c := range cases {
		if c.t.Valid() != c.ok {
			t.Errorf("%v.Valid() = %v, want %v", c.t, !c.ok, c.ok)
		}
	}
}

func TestParse(t *testing.T) {
	tl, ext, err := Parse("5", "15", "12.pbf", "pbf", "mvt")
	if err != nil || tl != (Tile{5, 15, 12}) || ext != "pbf" {
		t.Fatalf("got %v %q %v", tl, ext, err)
	}
	if _, _, err := Parse("5", "15", "12.png", "pbf"); !errors.Is(err, ErrInvalidTile) {
		t.Fatalf("expected extension error, got %v", err)
	}
	if _, _, err := Parse("2", "4", "0.pbf", "pbf"); !errors.Is(err, ErrInvalidTile) {
		t.Fatalf("expected range error, got %v", err)
	}
	if _, _, err := Parse("a", "0", "0"); !errors.Is(err, ErrInvalidTile) {
		t.Fatalf("expected parse error, got %v", err)
	}
	if tl, ext, err := Parse("0", "0", "0"); err != nil || ext != "" || tl != (Tile{}) {
		t.Fatalf("no extension: %v %q %v", tl, ext, err)
	}
}

func TestMercatorBoundsWorld(t *testing.T) {
	b := Tile{0, 0, 0}.MercatorBounds()
	const w = 20037508.342789244
	if !near(b.MinX, -w, 1e-6) || !near(b.MaxX, w, 1e-6) || !near(b.MinY, -w, 1e-6) || !near(b.MaxY, w, 1e-6) {
		t.Fatalf("world bounds %+v", b)
	}
}

func TestMercatorBoundsMatchesTileEnvelope(t *testing.T) {
	// ST_TileEnvelope(1, 1, 0) = BOX(0 0, 20037508.34 20037508.34)
	b := Tile{1, 1, 0}.MercatorBounds()
	if !near(b.MinX, 0, 1e-6) || !near(b.MinY, 0, 1e-6) || !near(b.MaxX, OriginShift, 1e-6) || !near(b.MaxY, OriginShift, 1e-6) {
		t.Fatalf("got %+v", b)
	}
	// Children tile their parent exactly.
	p := Tile{7, 64, 51}
	pb := p.MercatorBounds()
	for _, c := range p.Children() {
		cb := c.MercatorBounds()
		if cb.MinX < pb.MinX-1e-6 || cb.MaxX > pb.MaxX+1e-6 || cb.MinY < pb.MinY-1e-6 || cb.MaxY > pb.MaxY+1e-6 {
			t.Fatalf("child %v %+v outside parent %+v", c, cb, pb)
		}
		if c.Parent() != p {
			t.Fatalf("parent of %v = %v", c, c.Parent())
		}
	}
}

func TestLonLatBounds(t *testing.T) {
	b := Tile{0, 0, 0}.LonLatBounds()
	if !near(b.MinX, -180, 1e-9) || !near(b.MaxX, 180, 1e-9) || !near(b.MaxY, MaxLat, 1e-9) || !near(b.MinY, -MaxLat, 1e-9) {
		t.Fatalf("got %+v", b)
	}
	if !near(MaxLat, 85.0511287798, 1e-8) {
		t.Fatalf("MaxLat = %v", MaxLat)
	}
}

func TestFromLonLat(t *testing.T) {
	// Algiers (3.0588 E, 36.7538 N)
	cases := []struct {
		lon, lat float64
		z        int
		want     Tile
	}{
		{3.0588, 36.7538, 0, Tile{0, 0, 0}},
		{3.0588, 36.7538, 1, Tile{1, 1, 0}},
		{3.0588, 36.7538, 10, Tile{10, 520, 399}},
		{-0.1276, 51.5072, 12, Tile{12, 2046, 1362}}, // London
		{180, 90, 3, Tile{3, 7, 0}},                  // clamped
		{-180, -90, 3, Tile{3, 0, 7}},
	}
	for _, c := range cases {
		if got := FromLonLat(c.lon, c.lat, c.z); got != c.want {
			t.Errorf("FromLonLat(%v,%v,%d) = %v, want %v", c.lon, c.lat, c.z, got, c.want)
		}
	}
}

func TestRoundTripContainment(t *testing.T) {
	for z := 0; z <= 18; z += 3 {
		for _, p := range [][2]float64{{3.05, 36.75}, {-122.4, 37.8}, {151.2, -33.9}, {0, 0}} {
			tl := FromLonLat(p[0], p[1], z)
			b := tl.LonLatBounds()
			if p[0] < b.MinX || p[0] > b.MaxX || p[1] < b.MinY || p[1] > b.MaxY {
				t.Fatalf("point %v not inside %v bounds %+v", p, tl, b)
			}
			mx, my := LonLatToMercator(p[0], p[1])
			mb := tl.MercatorBounds()
			if mx < mb.MinX-1e-6 || mx > mb.MaxX+1e-6 || my < mb.MinY-1e-6 || my > mb.MaxY+1e-6 {
				t.Fatalf("mercator point (%v,%v) not inside %v %+v", mx, my, tl, mb)
			}
		}
	}
}

func TestLonLatToMercator(t *testing.T) {
	x, y := LonLatToMercator(0, 0)
	if !near(x, 0, 1e-9) || !near(y, 0, 1e-6) {
		t.Fatalf("origin -> %v %v", x, y)
	}
	x, y = LonLatToMercator(180, MaxLat)
	if !near(x, OriginShift, 1e-6) || !near(y, OriginShift, 1e-3) {
		t.Fatalf("corner -> %v %v", x, y)
	}
}
