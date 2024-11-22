package elevation

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
	"sync"

	"github.com/google/tiff"
	_ "github.com/google/tiff/bigtiff"
	_ "github.com/google/tiff/geotiff"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/image/tiff/lzw"
)

const noDataBits = 0xff7fffff

var (
	errShortRead = errors.New("short read")
	noData       = math.Float32frombits(noDataBits)
)

var (
	filesOpened = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_files_opened_total",
		Help: "The total number of files opened",
	})
	filesClosed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_files_closed_total",
		Help: "The total number of files closed",
	})
	emptyTileCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_empty_tile_cache_hits_total",
		Help: "The total number of hits on the empty tile cache",
	})
	emptyTileCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_empty_tile_cache_misses_total",
		Help: "The total number of misses on the empty tile cache",
	})
	tileCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_tile_cache_hits_total",
		Help: "The total number of hits on the tile cache",
	})
	tileCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_tile_cache_misses_total",
		Help: "The total number of misses on the tile cache",
	})
	tileCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "elevation_tile_cache_evictions_total",
		Help: "The total number of evictions from the tile cache",
	})
)

// A GeoTIFFTile is an open GeoTIFF file.
type GeoTIFFTile struct {
	mutex                     sync.Mutex
	file                      *os.File
	imageWidth                int
	imageLength               int
	tileWidth                 int
	tileLength                int
	tilesAcross               int
	tilesDown                 int
	tileOffsets               []uint64
	tileByteCounts            []uint64
	smallestTileByteCount     uint64
	tileSampleCount           int
	tileByteCountUncompressed int
	tileCacheSizeBytes        int
	tileCache                 *lru.Cache[TileCoord, []float32]
	emptyTileBytes            []byte
	emptyTiles                sync.Map
	scaleX                    int
	scaleY                    int
	translateX                int
	translateY                int
}

type GeoTIFFTileOption func(*GeoTIFFTile)

// A geoTIFFIFD is a struct into which github.com/google/tiff can unmarshal an
// IFD.
type geoTIFFIFD struct {
	ImageWidth                uint16    `tiff:"field,tag=256"`
	ImageLength               uint16    `tiff:"field,tag=257"`
	BitsPerSample             uint16    `tiff:"field,tag=258"`
	Compression               uint16    `tiff:"field,tag=259"`
	PhotometricInterpretation uint16    `tiff:"field,tag=262"`
	SamplesPerPixel           uint16    `tiff:"field,tag=277"`
	PlanarConfiguration       uint16    `tiff:"field,tag=284"`
	Predictor                 uint16    `tiff:"field,tag=317"`
	TileWidth                 uint16    `tiff:"field,tag=322"`
	TileLength                uint16    `tiff:"field,tag=323"`
	TileOffsets               []uint64  `tiff:"field,tag=324"`
	TileByteCounts            []uint64  `tiff:"field,tag=325"`
	SampleFormat              uint16    `tiff:"field,tag=339"`
	ModelPixelScaleTag        []float64 `tiff:"field,tag=33550"`
	ModelTiepointTag          []float64 `tiff:"field,tag=33922"`
	GeoKeyDirectoryTag        []uint16  `tiff:"field,tag=34735"`
	GeoDoubleParamsTag        []float64 `tiff:"field,tag=34736"`
	GeoASCIIParamsTag         string    `tiff:"field,tag=34737"`
	GDALMetadata              string    `tiff:"field,tag=42112"`
	GDALNoData                string    `tiff:"field,tag=42113"`
}

// NewGeoTIFFTile returns a new GeoTIFFTile.
func NewGeoTIFFTile(fsys fs.FS, filename string, options ...GeoTIFFTileOption) (*GeoTIFFTile, error) {
	var err error
	ok := false

	f := &GeoTIFFTile{
		tileCacheSizeBytes: 128 << 20, // 128MB.
	}
	for _, option := range options {
		option(f)
	}

	file, err := fsys.Open(filename)
	if err != nil {
		return nil, err
	}
	if _, ok := file.(*os.File); !ok {
		return nil, errors.ErrUnsupported
	}
	f.file = file.(*os.File)
	filesOpened.Inc()
	defer func() {
		if !ok {
			filesClosed.Inc()
			f.file.Close()
		}
	}()

	tiffTIFF, err := tiff.Parse(f.file, tiff.GetTagSpace("GeoTIFF"), nil)
	if err != nil {
		return nil, err
	}

	if len(tiffTIFF.IFDs()) != 1 {
		return nil, fmt.Errorf("found %d IFDs, expected 1", len(tiffTIFF.IFDs()))
	}

	var ifd geoTIFFIFD
	if err := tiff.UnmarshalIFD(tiffTIFF.IFDs()[0], &ifd); err != nil {
		return nil, err
	}

	if ifd.BitsPerSample != 32 ||
		ifd.Compression != 5 ||
		ifd.PhotometricInterpretation != 1 ||
		ifd.SamplesPerPixel != 1 ||
		ifd.PlanarConfiguration != 1 ||
		ifd.Predictor != 1 ||
		ifd.SampleFormat != 3 ||
		len(ifd.ModelPixelScaleTag) != 3 || ifd.ModelPixelScaleTag[2] != 0 ||
		len(ifd.ModelTiepointTag) != 6 || ifd.ModelTiepointTag[2] != 0 || ifd.ModelTiepointTag[5] != 0 ||
		ifd.GDALNoData != "-3.4028234663852886e+038" {
		return nil, errors.ErrUnsupported
	}

	f.imageWidth = int(ifd.ImageWidth)
	f.imageWidth = int(ifd.ImageWidth)
	f.imageLength = int(ifd.ImageLength)
	f.tileWidth = int(ifd.TileWidth)
	f.tileLength = int(ifd.TileLength)
	f.tilesAcross = (f.imageWidth + f.tileWidth - 1) / f.tileWidth
	f.tilesDown = (f.imageLength + f.tileLength - 1) / f.tileLength
	tilesPerImage := f.tilesAcross * f.tilesDown
	if len(ifd.TileByteCounts) != tilesPerImage || len(ifd.TileOffsets) != tilesPerImage {
		return nil, errors.New("incorrect number of tile byte counts or offsets")
	}
	f.tileOffsets = ifd.TileOffsets
	f.tileByteCounts = ifd.TileByteCounts
	f.smallestTileByteCount = ifd.TileByteCounts[0]
	for _, tileByteCount := range ifd.TileByteCounts[1:] {
		if tileByteCount < f.smallestTileByteCount {
			f.smallestTileByteCount = tileByteCount
		}
	}
	f.tileSampleCount = f.tileWidth * f.tileLength
	f.tileByteCountUncompressed = f.tileSampleCount * int(ifd.BitsPerSample) / 8

	tileCacheCount := max(f.tileCacheSizeBytes/f.tileByteCountUncompressed, 1)
	f.tileCache, err = lru.New[TileCoord, []float32](tileCacheCount)
	if err != nil {
		return nil, err
	}

	scaleX, scaleY, scaleZ := ifd.ModelPixelScaleTag[0], ifd.ModelPixelScaleTag[1], ifd.ModelPixelScaleTag[2]
	if scaleX != float64(int(scaleX)) || scaleY != float64(int(scaleY)) || scaleZ != 0 {
		return nil, errors.ErrUnsupported
	}
	i, j, k := ifd.ModelTiepointTag[0], ifd.ModelTiepointTag[1], ifd.ModelTiepointTag[2]
	if i != 0 || j != 0 || k != 0 {
		return nil, errors.ErrUnsupported
	}
	x, y, z := ifd.ModelTiepointTag[3], ifd.ModelTiepointTag[4], ifd.ModelTiepointTag[5]
	if x != float64(int(x)) || y != float64(int(y)) || z != 0 {
		return nil, errors.ErrUnsupported
	}
	f.scaleX = int(scaleX)
	f.scaleY = int(scaleY)
	f.translateX = int(x)
	f.translateY = int(y)

	ok = true
	return f, nil
}

func WithTileCacheSize(tileCacheSize int) GeoTIFFTileOption {
	return func(f *GeoTIFFTile) {
		f.tileCacheSizeBytes = tileCacheSize
	}
}

func (f *GeoTIFFTile) Close() error {
	filesClosed.Inc()
	return f.file.Close()
}

// Sample returns a single sample from f.
func (f *GeoTIFFTile) Sample(coord Coord) (float64, error) {
	localCoord := f.localCoord(coord)
	localTileCoord, ok := f.localTileCoord(localCoord)
	if !ok {
		return math.NaN(), nil
	}
	tileSamples, err := f.getTileSamplesCached(localTileCoord)
	if err != nil {
		return 0, err
	}
	return f.tileSample(tileSamples, localCoord), nil
}

// Samples returns multiple samples from f. It is significantly faster than
// calling [Sample] for each coordinate.
func (f *GeoTIFFTile) Samples(coords []Coord) ([]float64, error) {
	localCoords := make([]Coord, len(coords))
	for i, coord := range coords {
		localCoords[i] = f.localCoord(coord)
	}

	samples := make([]float64, len(localCoords))

	// Group indexes by local tile coord.
	indexesByLocalTileCoord := make(map[TileCoord][]int)
	for index, localCoord := range localCoords {
		localTileCoord, ok := f.localTileCoord(localCoord)
		if !ok {
			samples[index] = math.NaN()
			continue
		}
		indexesByLocalTileCoord[localTileCoord] = append(indexesByLocalTileCoord[localTileCoord], index)
	}

	// Populate samples one local tile at a time.
	for localTileCoord, indexes := range indexesByLocalTileCoord {
		tileSamples, err := f.getTileSamplesCached(localTileCoord)
		if err != nil {
			return nil, err
		}
		slices.Sort(indexes)
		for _, index := range indexes {
			samples[index] = f.tileSample(tileSamples, localCoords[index])
		}
	}

	return samples, nil
}

// getCompressedTileData returns the compressed tile data for the data at
// localTileCoord. If the tile is known to be empty, it returns nil.
func (f *GeoTIFFTile) getCompressedTileData(localTileCoord TileCoord) ([]byte, error) {
	tileIndex := localTileCoord.C + f.tilesAcross*localTileCoord.R
	tileByteCount := f.tileByteCounts[tileIndex]
	tileOffset := f.tileOffsets[tileIndex]
	compressedData := make([]byte, tileByteCount)
	switch n, err := f.file.ReadAt(compressedData, int64(tileOffset)); {
	case err != nil:
		return nil, err
	case n != int(tileByteCount):
		return nil, errShortRead
	case f.emptyTileBytes != nil && bytes.Equal(compressedData, f.emptyTileBytes):
		return nil, nil
	default:
		return compressedData, nil
	}
}

// decompressTileData decompresses the tile data in compressedData.
func (f *GeoTIFFTile) decompressTileData(compressedData []byte) ([]byte, error) {
	tileData := make([]byte, f.tileByteCountUncompressed)
	r := lzw.NewReader(bytes.NewReader(compressedData), lzw.MSB, 8)
	for bytesRead := 0; bytesRead < f.tileByteCountUncompressed; {
		n, err := r.Read(tileData[bytesRead:])
		if err != nil {
			return nil, err
		}
		bytesRead += n
	}
	return tileData, nil
}

// decodeTileData decodes tileData.
func (f *GeoTIFFTile) decodeTileData(tileData []byte) []float32 {
	tileSamples := make([]float32, f.tileSampleCount)
	for i := range f.tileSampleCount {
		b := binary.LittleEndian.Uint32(tileData[i*4 : (i+1)*4])
		tileSamples[i] = math.Float32frombits(b)
	}
	return tileSamples
}

// localCoord returns the local coordinate of coord.
func (t *GeoTIFFTile) localCoord(coord Coord) Coord {
	return Coord{
		X: (coord.X - t.translateX) / t.scaleX,
		Y: -(coord.Y - t.translateY) / t.scaleY,
	}
}

// getTileSamples returns the tile samples at localTileCoord.
func (f *GeoTIFFTile) getTileSamples(localTileCoord TileCoord) ([]float32, error) {
	// Retrieve the compressed tile data.
	compressedTileData, err := f.getCompressedTileData(localTileCoord)
	if err != nil {
		return nil, err
	}

	// If the tile is empty return immediately.
	if compressedTileData == nil {
		return nil, nil
	}

	// Otherwise, decompress the tile data and decode it.
	tileData, err := f.decompressTileData(compressedTileData)
	if err != nil {
		return nil, err
	}
	tileSamples := f.decodeTileData(tileData)

	// If we do not know what an empty tile looks like compressed, check to see
	// if this is an empty tile, and, if so, use its bytes to detect empty tiles
	// before they are decompressed. We assume that the empty tile is the
	// smallest tile.
	if f.emptyTileBytes == nil && len(compressedTileData) == int(f.smallestTileByteCount) {
		isEmptyTile := true
		for _, sample := range tileSamples {
			if sample != noData {
				isEmptyTile = false
				break
			}
		}
		if isEmptyTile {
			f.emptyTileBytes = compressedTileData
			return nil, nil
		}
	}

	return tileSamples, nil
}

// tileSamplesCache returns the tile at localTileCoord using f's cache.
func (f *GeoTIFFTile) getTileSamplesCached(localTileCoord TileCoord) ([]float32, error) {
	// Check if the tile is known to be empty.
	if _, ok := f.emptyTiles.Load(localTileCoord); ok {
		emptyTileCacheHits.Inc()
		return nil, nil
	}

	// Get tile samples from the cache, if possible.
	if tileSamples, ok := f.tileCache.Get(localTileCoord); ok {
		tileCacheHits.Inc()
		return tileSamples, nil
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()

	// Check if the tile is known to be empty.
	if _, ok := f.emptyTiles.Load(localTileCoord); ok {
		emptyTileCacheHits.Inc()
		return nil, nil
	}

	// Retry getting tile samples from the cache, in case the cache was populated
	// while the mutex was locked.
	if tileSamples, ok := f.tileCache.Get(localTileCoord); ok {
		tileCacheHits.Inc()
		return tileSamples, nil
	}

	// Otherwise, retrieve the tile samples and populate the cache.
	tileSamples, err := f.getTileSamples(localTileCoord)
	if err != nil {
		return nil, err
	}

	// Store the samples, either as a known empty tile or in the cache.
	if tileSamples == nil {
		f.emptyTiles.Store(localTileCoord, struct{}{})
		emptyTileCacheMisses.Inc()
	} else {
		if eviction := f.tileCache.Add(localTileCoord, tileSamples); eviction {
			tileCacheEvictions.Inc()
		}
		tileCacheMisses.Inc()
	}

	return tileSamples, nil
}

// localTileCoord returns the local tile coord for a given coordinate.
func (f *GeoTIFFTile) localTileCoord(localCoord Coord) (TileCoord, bool) {
	if localCoord.X < 0 || f.imageWidth <= localCoord.X || localCoord.Y < 0 || f.imageLength <= localCoord.Y {
		return TileCoord{}, false
	}
	return TileCoord{
		C: localCoord.X / f.tileWidth,
		R: localCoord.Y / f.tileLength,
	}, true
}

// tileSample returns the sample from tileSamples at localCoord.
func (f *GeoTIFFTile) tileSample(tileSamples []float32, localCoord Coord) float64 {
	if tileSamples == nil {
		return math.NaN()
	}
	sample := tileSamples[localCoord.X%f.tileWidth+(localCoord.Y%f.tileLength)*f.tileWidth]
	if sample == noData {
		return math.NaN()
	}
	return float64(sample)
}
