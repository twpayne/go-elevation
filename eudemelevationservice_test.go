package elevation_test

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/twpayne/go-elevation"
)

func TestEUDEMElevationService_Elevation4326(t *testing.T) {
	fsys := os.DirFS("testdata/eu_dem")
	euDEMElevationService, err := elevation.NewEUDEMElevationService(fsys)
	assert.NoError(t, err)

	for _, tc := range []struct {
		name     string
		filename string
		coord    []float64
		expected float64
	}{
		{
			name:     "azores",
			filename: "eu_dem_v11_E00N20.TIF",
			coord:    []float64{-31.216667, 39.466667},
			expected: 836.8908398692249,
		},
		{
			name:     "la_plagne",
			filename: "eu_dem_v11_E40N20.TIF",
			coord:    []float64{6.6771972, 45.505288300000004},
			expected: 1985.4962777956653,
		},
		{
			name:     "null_island",
			coord:    []float64{0, 0},
			expected: math.NaN(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.filename != "" {
				if _, err := fsys.(fs.StatFS).Stat(tc.filename); errors.Is(err, fs.ErrNotExist) {
					t.Skip(err)
				} else {
					assert.NoError(t, err)
				}
			}
			actual, err := euDEMElevationService.Elevation4326(t.Context(), [][]float64{tc.coord})
			assert.NoError(t, err)
			if math.IsNaN(tc.expected) {
				assert.Equal(t, 1, len(actual))
				assert.True(t, math.IsNaN(actual[0]))
			} else {
				assert.Equal(t, []float64{tc.expected}, actual)
			}
		})
	}
}
