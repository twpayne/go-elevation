package elevation

import (
	"errors"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestNewGeoTIFFTile(t *testing.T) {
	geoTIFFTile, err := NewGeoTIFFTile(os.DirFS("testdata/eu_dem"), "eu_dem_v11_E00N20.TIF")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip(err)
	}
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, geoTIFFTile.Close())
	}()

	visitAllTiles(t, geoTIFFTile)

	testSampleSamplesEquivalence(t, geoTIFFTile)
}

func TestGeoTIFFFile_Sample(t *testing.T) {
	geoTIFFTile, err := NewGeoTIFFTile(os.DirFS("testdata/eu_dem"), "eu_dem_v11_E00N20.TIF")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip(err)
	}
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, geoTIFFTile.Close())
	}()

	testCases := []struct {
		coord    Coord
		expected float64
	}{
		{coord: Coord{X: 970705, Y: 2789764}, expected: 517}, // QGIS says 518
		{coord: Coord{X: 971739, Y: 2793094}, expected: 79},
		{coord: Coord{X: 969236, Y: 2787499}, expected: 6},   // QGIS says 13
		{coord: Coord{X: 950258, Y: 2769570}, expected: 586}, // QGIS says 593
	}

	for _, tc := range testCases {
		actual, err := geoTIFFTile.Sample(tc.coord)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, actual)
	}

	coords := make([]Coord, len(testCases))
	expected := make([]float64, len(testCases))
	for i, tc := range testCases {
		coords[i] = tc.coord
		expected[i] = tc.expected
	}
	actual, err := geoTIFFTile.Samples(coords)
	assert.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestGeoTIFFFile_Pixel(t *testing.T) {
	geoTIFFTile, err := NewGeoTIFFTile(os.DirFS("testdata/eu_dem"), "eu_dem_v11_E00N20.TIF")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip(err)
	}
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, geoTIFFTile.Close())
	}()

	for _, tc := range []struct {
		center   Coord
		expected []float64
	}{
		{
			center: Coord{X: 954537, Y: 2777462},
			expected: []float64{
				0,
				math.NaN(),
				math.NaN(),
				0,
				math.NaN(),
			},
		},
		{
			center: Coord{X: 952662, Y: 2770962},
			expected: []float64{
				530,
				532,
				524,
				529,
				537,
			},
		},
	} {
		coords := []Coord{
			tc.center,
			{X: tc.center.X, Y: tc.center.Y + 25},
			{X: tc.center.X + 25, Y: tc.center.Y},
			{X: tc.center.X, Y: tc.center.Y - 25},
			{X: tc.center.X - 25, Y: tc.center.Y},
		}
		actual, err := geoTIFFTile.Samples(coords)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, actual)
	}
}

func visitAllTiles(t *testing.T, f *GeoTIFFTile) {
	t.Helper()
	for r := range f.tilesDown {
		for c := range f.tilesAcross {
			_, err := f.getTileSamplesCached(TileCoord{C: c, R: r})
			assert.NoError(t, err)
		}
	}
}

func testSampleSamplesEquivalence(t *testing.T, f *GeoTIFFTile) {
	t.Helper()
	r := rand.New(rand.NewPCG(0, 0))
	for range 16384 {
		n := r.IntN(16)
		coords := make([]Coord, n)
		for i := range len(coords) {
			coords[i] = Coord{
				X: f.translateX + r.IntN(f.imageWidth*f.scaleX),
				Y: f.translateY + f.imageLength*f.scaleY - r.IntN(f.imageLength*f.scaleY),
			}
		}
		sampleCoords := make([]float64, n)
		for i, coord := range coords {
			var err error
			sampleCoords[i], err = f.Sample(coord)
			assert.NoError(t, err)
		}
		samplesCoords, err := f.Samples(coords)
		assert.NoError(t, err)
		assert.Equal(t, sampleCoords, samplesCoords)
	}
}
