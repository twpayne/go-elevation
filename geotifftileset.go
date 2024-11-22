package elevation

// FIXME it's rasters all the way down: instead of TileSet/GeoTIFFTile/Tile make it a hierarchy
// FIXME rasters all implement Sample() and Samples() and pass this down the tree if needed
// FIXME need to add an interface method to group related samples together
// FIXME check possible off-by-one error compared to QGIS
// FIXME interpolation

import (
	"errors"
	"io/fs"
	"math"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	missingTileCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_missing_tile_cache_hits_total",
		Help: "The total number of hits on the missing tile cache",
	})
	missingTileCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_missing_tile_cache_misses_total",
		Help: "The total number of misses on the missing tile cache",
	})
	globalTileCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_global_tile_cache_hits_total",
		Help: "The total number of hits on the global tile cache",
	})
	globalTileCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_global_tile_cache_misses_total",
		Help: "The total number of misses on the global tile cache",
	})
	globalTileCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_global_tile_cache_evictions_total",
		Help: "The total number of evictions from the global tile cache",
	})
)

// A TileCoordFunc returns the tile coordinate for a coordinate.
type TileCoordFunc func(Coord) (TileCoord, bool)

// A TileFilenameFunc returns the tile filename for a tile coordinate.
type TileFilenameFunc func(TileCoord) string

// A GeoTIFFTileSet is a set of GeoTIFF tiles.
type GeoTIFFTileSet struct {
	mutex              sync.Mutex
	fsys               fs.FS
	srid               int
	tileCoordFunc      TileCoordFunc
	tileFilenameFunc   TileFilenameFunc
	missingTiles       sync.Map
	geoTIFFTileOptions []GeoTIFFTileOption
	cacheSize          int
	scaleX             int
	scaleY             int
	geoTIFFTileCache   *lru.Cache[TileCoord, *GeoTIFFTile]
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

	var err error
	s.geoTIFFTileCache, err = lru.NewWithEvict(s.cacheSize, func(key TileCoord, value *GeoTIFFTile) {
		value.Close()
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
func (s *GeoTIFFTileSet) Samples(coords []Coord) ([]float64, error) {
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
		tile, err := s.getTileCached(tileCoord)
		if err != nil {
			return nil, err
		}
		if tile == nil {
			for _, index := range group.indexes {
				samples[index] = math.NaN()
			}
			continue
		}
		localSamples, err := tile.Samples(group.coords)
		if err != nil {
			return nil, err
		}
		for localIndex, index := range group.indexes {
			samples[index] = localSamples[localIndex]
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
func (s *GeoTIFFTileSet) getTile(tileCoord TileCoord) (*GeoTIFFTile, error) {
	filename := s.tileFilenameFunc(tileCoord)
	switch geoTIFFTile, err := NewGeoTIFFTile(s.fsys, filename, s.geoTIFFTileOptions...); {
	case errors.Is(err, fs.ErrNotExist):
		s.missingTiles.Store(tileCoord, struct{}{})
		missingTileCacheMisses.Inc()
		return nil, nil
	case err != nil:
		return nil, err
	default:
		return geoTIFFTile, nil
	}
}

// getTileCached returns the tile at the give tile coordinate, using the cache
// if possible.
func (s *GeoTIFFTileSet) getTileCached(tileCoord TileCoord) (*GeoTIFFTile, error) {
	if _, ok := s.missingTiles.Load(tileCoord); ok {
		missingTileCacheHits.Inc()
		return nil, nil
	}

	if tile, ok := s.geoTIFFTileCache.Get(tileCoord); ok {
		globalTileCacheHits.Inc()
		return tile, nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, ok := s.missingTiles.Load(tileCoord); ok {
		missingTileCacheHits.Inc()
		return nil, nil
	}

	if tile, ok := s.geoTIFFTileCache.Get(tileCoord); ok {
		globalTileCacheHits.Inc()
		return tile, nil
	}

	globalTileCacheMisses.Inc()

	tile, err := s.getTile(tileCoord)
	if err != nil {
		return nil, err
	}

	if eviction := s.geoTIFFTileCache.Add(tileCoord, tile); eviction {
		globalTileCacheEvictions.Inc()
	}

	return tile, nil
}
