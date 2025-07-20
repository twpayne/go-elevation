package elevation

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"

	"github.com/google/tiff"
	_ "github.com/google/tiff/bigtiff"
	_ "github.com/google/tiff/geotiff"
	"github.com/maypok86/otter/v2"
	"golang.org/x/image/tiff/lzw"
)

const noDataBits = 0xff7fffff

var (
	errShortRead = errors.New("short read")
	noData       = math.Float32frombits(noDataBits)
)

// A GeoTIFFTile is an open GeoTIFF file.
type GeoTIFFTile struct {
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
	tileSamplesCache          *otter.Cache[TileCoord, []float32]
	emptyTileBytes            []byte
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
	defer func() {
		if !ok {
			_ = f.file.Close()
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
	f.tileSamplesCache, err = otter.New(&otter.Options[TileCoord, []float32]{
		MaximumSize: tileCacheCount,
	})
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
	return f.file.Close()
}

// Sample returns a single sample from f.
func (f *GeoTIFFTile) Sample(ctx context.Context, coord Coord) (float64, error) {
	localCoord := f.localCoord(coord)
	localTileCoord, ok := f.localTileCoord(localCoord)
	if !ok {
		return math.NaN(), nil
	}
	switch tileSamples, err := f.getTileSamplesCached(ctx, localTileCoord); {
	case errors.Is(err, otter.ErrNotFound):
		return math.NaN(), nil
	case err != nil:
		return 0, err
	default:
		return f.tileSample(tileSamples, localCoord), nil
	}
}

// Samples returns multiple samples from f. It is significantly faster than
// calling [Sample] for each coordinate.
func (f *GeoTIFFTile) Samples(ctx context.Context, coords []Coord) ([]float64, error) {
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
		slices.Sort(indexes)
		switch tileSamples, err := f.getTileSamplesCached(ctx, localTileCoord); {
		case errors.Is(err, otter.ErrNotFound):
			for _, index := range indexes {
				samples[index] = math.NaN()
			}
		case err != nil:
			return nil, err
		default:
			for _, index := range indexes {
				samples[index] = f.tileSample(tileSamples, localCoords[index])
			}
		}
	}

	return samples, nil
}

// getCompressedTileData returns the compressed tile data for the data at
// localTileCoord. If the tile is known to be empty, it returns the error
// otter.ErrNotFound.
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
		return nil, otter.ErrNotFound
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
func (f *GeoTIFFTile) getTileSamples(ctx context.Context, localTileCoord TileCoord) ([]float32, error) {
	// Retrieve the compressed tile data.
	compressedTileData, err := f.getCompressedTileData(localTileCoord)
	if err != nil {
		return nil, err
	}

	// Decompress the tile data and decode it.
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
			return nil, otter.ErrNotFound
		}
	}

	return tileSamples, nil
}

// tileSamplesCache returns the tile at localTileCoord using f's cache.
func (f *GeoTIFFTile) getTileSamplesCached(ctx context.Context, localTileCoord TileCoord) ([]float32, error) {
	return f.tileSamplesCache.Get(ctx, localTileCoord, otter.LoaderFunc[TileCoord, []float32](f.getTileSamples))
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
	sample := tileSamples[localCoord.X%f.tileWidth+(localCoord.Y%f.tileLength)*f.tileWidth]
	if sample == noData {
		return math.NaN()
	}
	return float64(sample)
}
