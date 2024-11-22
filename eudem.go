package elevation

import (
	"fmt"
	"io/fs"
	"slices"
)

func NewEUDEM(fsys fs.FS, options ...GeoTIFFTileSetOption) (*GeoTIFFTileSet, error) {
	return NewGeoTIFFTileSet(slices.Concat(
		[]GeoTIFFTileSetOption{
			WithFS(fsys),
			WithSRID(3035),
			WithScale(25, 25),
			WithTileCoordFunc(func(coord Coord) (TileCoord, bool) {
				return TileCoord{
					C: 10 * (coord.X / 1000000),
					R: 10 * (coord.Y / 1000000),
				}, true
			}),
			WithTileFilenameFunc(func(tileCoord TileCoord) string {
				return fmt.Sprintf("eu_dem_v11_E%02dN%02d.TIF", tileCoord.C, tileCoord.R)
			}),
		},
		options,
	)...)
}
