package elevation_test

import (
	"errors"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/twpayne/go-elevation"
)

func TestEUDEM_Samples(t *testing.T) {
	if _, err := os.Stat("testdata/eu_dem"); errors.Is(err, fs.ErrNotExist) {
		t.Skip("missing eu_dem test data")
	}

	fsys := os.DirFS("testdata/eu_dem")
	euDEM, err := elevation.NewEUDEM(fsys)
	assert.NoError(t, err)

	for i, tc := range []struct {
		requiredFiles []string
		coords        []elevation.Coord
		expected      []float64
	}{
		{
			requiredFiles: []string{
				"eu_dem_v11_E00N20.TIF",
			},
			coords: []elevation.Coord{
				{X: 970705, Y: 2789764},
				{X: 971739, Y: 2793094},
				{X: 969236, Y: 2787499},
				{X: 950258, Y: 2769570},
			},
			expected: []float64{
				517, // QGIS says 518.
				79,
				6,   // QGIS says 13.
				586, // QGIS says 593.
			},
		},
		{
			requiredFiles: []string{
				"eu_dem_v11_E30N50.TIF",
			},
			coords: []elevation.Coord{
				{X: 3030012, Y: 5003477},
				{X: 3073197, Y: 5027135},
				{X: 3175655, Y: 5026595},
			},
			expected: []float64{
				1141.1373291015625, // QGIS says 1136.0043.
				892.5265502929688,  // QGIS says 889.7675.
				94.63605499267578,  // QGIS says 92.92097.
			},
		},
		{
			requiredFiles: []string{
				"eu_dem_v11_E00N20.TIF",
				"eu_dem_v11_E30N50.TIF",
			},
			coords: []elevation.Coord{
				{X: 970705, Y: 2789764},
				{X: 3030012, Y: 5003477},
				{X: 971739, Y: 2793094},
				{X: 3073197, Y: 5027135},
				{X: 969236, Y: 2787499},
				{X: 3175655, Y: 5026595},
				{X: 950258, Y: 2769570},
			},
			expected: []float64{
				517,                // QGIS says 518.
				1141.1373291015625, // QGIS says 1136.0043.
				79,
				892.5265502929688, // QGIS says 889.7675.
				6,                 // QGIS says 13.
				94.63605499267578, // QGIS says 92.92097.
				586,               // QGIS says 593.
			},
		},
		{
			requiredFiles: []string{
				"eu_dem_v11_E40N20.TIF",
			},
			coords: []elevation.Coord{
				{X: 4077237, Y: 2529389},
				{X: 4076693, Y: 2596393},
				{X: 4207185, Y: 2673691},
			},
			expected: []float64{
				4712.9130859375,
				371.88299560546875,
				410.583984375,
			},
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			for _, filename := range tc.requiredFiles {
				if _, err := fsys.(fs.StatFS).Stat(filename); errors.Is(err, fs.ErrNotExist) {
					t.Skip(err)
				}
			}
			actual, err := euDEM.Samples(t.Context(), tc.coords)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func BenchmarkSingleTileSingleSample(b *testing.B) {
	r := rand.New(rand.NewPCG(0, 0))
	euDEM, err := elevation.NewEUDEM(os.DirFS("testdata/eu_dem"))
	assert.NoError(b, err)
	b.ResetTimer()
	for range b.N {
		samples, err := euDEM.Samples(b.Context(), []elevation.Coord{
			{
				X: 947000 + r.IntN(7000),
				Y: 2766000 + r.IntN(7000),
			},
		})
		assert.NoError(b, err)
		assert.Equal(b, 1, len(samples))
		assert.False(b, math.IsNaN(samples[0]))
	}
}

func BenchmarkSingleTileSixteenCloseSamples(b *testing.B) {
	r := rand.New(rand.NewPCG(0, 0))
	euDEM, err := elevation.NewEUDEM(os.DirFS("testdata/eu_dem"))
	assert.NoError(b, err)
	b.ResetTimer()
	for range b.N {
		coords := make([]elevation.Coord, 16)
		for i := range coords {
			coords[i] = elevation.Coord{
				X: 947000 + r.IntN(7000),
				Y: 2766000 + r.IntN(7000),
			}
		}
		samples, err := euDEM.Samples(b.Context(), coords)
		assert.NoError(b, err)
		assert.Equal(b, len(coords), len(samples))
		for _, sample := range samples {
			assert.False(b, math.IsNaN(sample))
		}
	}
}
