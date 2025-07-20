package elevation_test

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/twpayne/go-elevation"
)

func TestEUDEMElevationService(t *testing.T) {
	fsys := os.DirFS("testdata/eu_dem")
	if _, err := fsys.(fs.StatFS).Stat("eu_dem_v11_E00N20.TIF"); errors.Is(err, fs.ErrNotExist) {
		t.Skip(err)
	}
	euDEMElevationService, err := elevation.NewEUDEMElevationService(fsys)
	assert.NoError(t, err)

	actual, err := euDEMElevationService.Elevation4326(t.Context(), [][]float64{
		{-31.216667, 39.466667},
	})
	assert.NoError(t, err)
	assert.Equal(t, []float64{836.8908398692249}, actual)
}
