// Package tile implements XYZ / Web Mercator tile math used by the server.
//
// Tiles follow the "slippy map" convention: z is the zoom level, x grows
// eastwards from -180°, y grows southwards from ~85.0511°N.
package tile

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MaxZoom is the deepest zoom level the server accepts.
const MaxZoom = 24

// EarthRadius is the WGS84 semi-major axis used by EPSG:3857.
const EarthRadius = 6378137.0

// OriginShift is half the circumference of the EPSG:3857 world square.
var OriginShift = math.Pi * EarthRadius

// MaxLat is the latitude where Web Mercator is clipped.
var MaxLat = 180 / math.Pi * math.Atan(math.Sinh(math.Pi))

// Tile addresses one XYZ tile.
type Tile struct {
	Z, X, Y int
}

// ErrInvalidTile is returned when a tile address is out of range.
var ErrInvalidTile = errors.New("invalid tile address")

// Valid reports whether the tile lies inside the tile pyramid.
func (t Tile) Valid() bool {
	if t.Z < 0 || t.Z > MaxZoom {
		return false
	}
	n := 1 << t.Z
	return t.X >= 0 && t.X < n && t.Y >= 0 && t.Y < n
}

func (t Tile) String() string { return fmt.Sprintf("%d/%d/%d", t.Z, t.X, t.Y) }

// Parse parses "z", "x" and "y" path parts. y may carry an extension
// (e.g. "12.pbf"); allowed extensions are listed in exts (without dot).
// It returns the tile and the extension that was used.
func Parse(z, x, y string, exts ...string) (Tile, string, error) {
	ext := ""
	if i := strings.IndexByte(y, '.'); i >= 0 {
		ext = y[i+1:]
		y = y[:i]
		ok := len(exts) == 0
		for _, e := range exts {
			if e == ext {
				ok = true
				break
			}
		}
		if !ok {
			return Tile{}, "", fmt.Errorf("%w: unsupported extension %q", ErrInvalidTile, ext)
		}
	}
	zi, err1 := strconv.Atoi(z)
	xi, err2 := strconv.Atoi(x)
	yi, err3 := strconv.Atoi(y)
	if err := errors.Join(err1, err2, err3); err != nil {
		return Tile{}, "", fmt.Errorf("%w: %v", ErrInvalidTile, err)
	}
	t := Tile{zi, xi, yi}
	if !t.Valid() {
		return Tile{}, "", fmt.Errorf("%w: %s out of range", ErrInvalidTile, t)
	}
	return t, ext, nil
}

// Bounds is a rectangle (minX, minY, maxX, maxY).
type Bounds struct {
	MinX, MinY, MaxX, MaxY float64
}

// MercatorBounds returns the tile extent in EPSG:3857 metres. It matches
// PostGIS ST_TileEnvelope(z, x, y).
func (t Tile) MercatorBounds() Bounds {
	size := 2 * OriginShift / float64(int(1)<<t.Z)
	minX := -OriginShift + float64(t.X)*size
	maxY := OriginShift - float64(t.Y)*size
	return Bounds{minX, maxY - size, minX + size, maxY}
}

// LonLatBounds returns the tile extent in WGS84 degrees.
func (t Tile) LonLatBounds() Bounds {
	n := float64(int(1) << t.Z)
	lon := func(x int) float64 { return float64(x)/n*360 - 180 }
	lat := func(y int) float64 {
		return 180 / math.Pi * math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/n)))
	}
	return Bounds{lon(t.X), lat(t.Y + 1), lon(t.X + 1), lat(t.Y)}
}

// FromLonLat returns the tile containing the given position at zoom z.
// Latitudes beyond the Web Mercator limit are clamped.
func FromLonLat(lon, lat float64, z int) Tile {
	lat = math.Max(-MaxLat, math.Min(MaxLat, lat))
	n := float64(int(1) << z)
	x := int(math.Floor((lon + 180) / 360 * n))
	r := lat * math.Pi / 180
	y := int(math.Floor((1 - math.Log(math.Tan(r)+1/math.Cos(r))/math.Pi) / 2 * n))
	clamp := func(v int) int { return max(0, min(int(n)-1, v)) }
	return Tile{z, clamp(x), clamp(y)}
}

// LonLatToMercator projects WGS84 degrees to EPSG:3857 metres.
func LonLatToMercator(lon, lat float64) (float64, float64) {
	lat = math.Max(-MaxLat, math.Min(MaxLat, lat))
	x := lon * OriginShift / 180
	y := math.Log(math.Tan((90+lat)*math.Pi/360)) * EarthRadius
	return x, y
}

// Parent returns the tile one zoom level up.
func (t Tile) Parent() Tile {
	if t.Z == 0 {
		return t
	}
	return Tile{t.Z - 1, t.X / 2, t.Y / 2}
}

// Children returns the four tiles one zoom level down.
func (t Tile) Children() [4]Tile {
	z, x, y := t.Z+1, t.X*2, t.Y*2
	return [4]Tile{{z, x, y}, {z, x + 1, y}, {z, x, y + 1}, {z, x + 1, y + 1}}
}
