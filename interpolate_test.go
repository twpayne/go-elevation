package elevation_test

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/twpayne/go-elevation"
)

type testRaster struct {
	scaleX  int
	scaleY  int
	samples [][]float64
}

func (t *testRaster) Samples(ctx context.Context, coords []elevation.Coord) ([]float64, error) {
	samples := make([]float64, len(coords))
	for i, coord := range coords {
		samples[i] = t.samples[coord.Y/t.scaleY][coord.X/t.scaleX]
	}
	return samples, nil
}

func (t *testRaster) Scale() (int, int) {
	return t.scaleX, t.scaleY
}

func TestInterpolateBilinear(t *testing.T) {
	simpleRaster := &testRaster{
		scaleX: 10,
		scaleY: 10,
		samples: [][]float64{
			{0, 1, 2},
			{2, 3, 4},
			{4, 5, 6},
		},
	}
	for _, tc := range []struct {
		raster   elevation.Raster
		coords   [][]float64
		expected []float64
	}{
		{
			raster: simpleRaster,
			coords: [][]float64{
				{0, 0},
				{10, 0},
				{0, 10},
				{10, 10},
				{5, 5},
				{5, 0},
				{0, 5},
				{10, 5},
				{5, 10},
			},
			expected: []float64{
				0,
				1,
				2,
				3,
				1.5,
				0.5,
				1,
				2,
				2.5,
			},
		},
	} {
		actual, err := elevation.InterpolateBilinear(t.Context(), tc.raster, tc.coords)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, actual)
	}
}
