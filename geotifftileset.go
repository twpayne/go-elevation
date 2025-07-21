package elevation

// FIXME it's rasters all the way down: instead of TileSet/GeoTIFFTile/Tile make it a hierarchy
// FIXME rasters all implement Sample() and Samples() and pass this down the tree if needed
// FIXME need to add an interface method to group related samples together
// FIXME check possible off-by-one error compared to QGIS
// FIXME interpolation

import (
	"context"
	"errors"
	"io/fs"
	"math"

	"github.com/maypok86/otter/v2"
)

// A TileCoordFunc returns the tile coordinate for a coordinate.
type TileCoordFunc func(Coord) (TileCoord, bool)

// A TileFilenameFunc returns the tile filename for a tile coordinate.
type TileFilenameFunc func(TileCoord) string

// A GeoTIFFTileSet is a set of GeoTIFF tiles.
type GeoTIFFTileSet struct {
	fsys               fs.FS
	canaryFilename     string
	srid               int
	tileCoordFunc      TileCoordFunc
	tileFilenameFunc   TileFilenameFunc
	geoTIFFTileOptions []GeoTIFFTileOption
	cacheSize          int
	scaleX             int
	scaleY             int
	geoTIFFTileCache   *otter.Cache[TileCoord, *GeoTIFFTile]
}

// A GeoTIFFTileSetOption sets an option on a GeoTIFFTileSet.
type GeoTIFFTileSetOption func(*GeoTIFFTileSet)

// NewGeoTIFFileSet returns a new GeoTIFFTileSet with the given options.
func NewGeoTIFFTileSet(options ...GeoTIFFTileSetOption) (*GeoTIFFTileSet, error) {
	s := &GeoTIFFTileSet{
		cacheSize: 32,
	}
	for _, option := range options {
		option(s)
	}

	// If a canary filename is set, check that it can be opened.
	if s.canaryFilename != "" {
		file, err := s.fsys.Open(s.canaryFilename)
		if err != nil {
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
	}

	var err error
	s.geoTIFFTileCache, err = otter.New(&otter.Options[TileCoord, *GeoTIFFTile]{
		MaximumSize: s.cacheSize,
		OnDeletion: func(e otter.DeletionEvent[TileCoord, *GeoTIFFTile]) {
			_ = e.Value.Close()
		},
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func WithCacheSize(cacheSize int) GeoTIFFTileSetOption {
	return func(s *GeoTIFFTileSet) {
		s.cacheSize = cacheSize
	}
}

func WithCanaryFilename(canaryFilename string) GeoTIFFTileSetOption {
	return func(s *GeoTIFFTileSet) {
		s.canaryFilename = canaryFilename
	}
}

func WithFS(fsys fs.FS) GeoTIFFTileSetOption {
	return func(s *GeoTIFFTileSet) {
		s.fsys = fsys
	}
}

func WithGeoTIFFTileOptions(geoTIFFTileOptions ...GeoTIFFTileOption) GeoTIFFTileSetOption {
	return func(s *GeoTIFFTileSet) {
		s.geoTIFFTileOptions = geoTIFFTileOptions
	}
}

func WithTileCoordFunc(tileCoordFunc TileCoordFunc) GeoTIFFTileSetOption {
	return func(s *GeoTIFFTileSet) {
		s.tileCoordFunc = tileCoordFunc
	}
}

func WithSRID(srid int) GeoTIFFTileSetOption {
	return func(s *GeoTIFFTileSet) {
		s.srid = srid
	}
}

func WithScale(scaleX, scaleY int) GeoTIFFTileSetOption {
	return func(s *GeoTIFFTileSet) {
		s.scaleX = scaleX
		s.scaleY = scaleY
	}
}

func WithTileFilenameFunc(tileFilenameFunc TileFilenameFunc) GeoTIFFTileSetOption {
	return func(s *GeoTIFFTileSet) {
		s.tileFilenameFunc = tileFilenameFunc
	}
}

// Samples returns the samples at coords. Missing samples are represented by
// NaNs.
func (s *GeoTIFFTileSet) Samples(ctx context.Context, coords []Coord) ([]float64, error) {
	samples := make([]float64, len(coords))

	// Group indexes by tile coord.
	type groupStruct struct {
		coords  []Coord
		indexes []int
	}
	groupsByTileCoord := make(map[TileCoord]groupStruct)
	for index, coord := range coords {
		tileCoord, ok := s.tileCoordFunc(coord)
		if !ok {
			samples[index] = math.NaN()
			continue
		}
		if group, ok := groupsByTileCoord[tileCoord]; ok {
			group.coords = append(group.coords, coord)
			group.indexes = append(group.indexes, index)
			groupsByTileCoord[tileCoord] = group
		} else {
			group := groupStruct{
				coords:  []Coord{coord},
				indexes: []int{index},
			}
			groupsByTileCoord[tileCoord] = group
		}
	}

	// Populate samples one tile at a time.
	for tileCoord, group := range groupsByTileCoord {
		switch tile, err := s.getTileCached(ctx, tileCoord); {
		case errors.Is(err, otter.ErrNotFound):
			for _, index := range group.indexes {
				samples[index] = math.NaN()
			}
		case err != nil:
			return nil, err
		default:
			localSamples, err := tile.Samples(ctx, group.coords)
			if err != nil {
				return nil, err
			}
			for localIndex, index := range group.indexes {
				samples[index] = localSamples[localIndex]
			}
		}
	}

	return samples, nil
}

// SRID returns s's SRID.
func (s *GeoTIFFTileSet) SRID() int {
	return s.srid
}

// Scale returns s's scale.
func (s *GeoTIFFTileSet) Scale() (int, int) {
	return s.scaleX, s.scaleY
}

// getTile returns the tile at the given tile coordinate.
func (s *GeoTIFFTileSet) getTile(ctx context.Context, tileCoord TileCoord) (*GeoTIFFTile, error) {
	filename := s.tileFilenameFunc(tileCoord)
	switch geoTIFFTile, err := NewGeoTIFFTile(s.fsys, filename, s.geoTIFFTileOptions...); {
	case errors.Is(err, fs.ErrNotExist):
		return nil, otter.ErrNotFound
	case err != nil:
		return nil, err
	default:
		return geoTIFFTile, nil
	}
}

// getTileCached returns the tile at the give tile coordinate, using the cache
// if possible.
func (s *GeoTIFFTileSet) getTileCached(ctx context.Context, tileCoord TileCoord) (*GeoTIFFTile, error) {
	return s.geoTIFFTileCache.Get(ctx, tileCoord, otter.LoaderFunc[TileCoord, *GeoTIFFTile](s.getTile))
}
