package elevation

import "context"

// A Coord is a coordinate.
type Coord struct {
	X int
	Y int
}

// A TileCoord is a tile coordinate.
type TileCoord struct {
	C int // Column.
	R int // Row.
}

type Raster interface {
	Samples(ctx context.Context, coords []Coord) ([]float64, error)
	Scale() (int, int)
}
