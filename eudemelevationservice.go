package elevation

import (
	"io/fs"

	"github.com/twpayne/go-proj/v11"
)

type EUDEMElevationService struct {
	geoTIFFFileSet *GeoTIFFTileSet
	pj             *proj.PJ
}

func NewEUDEMElevationService(fsys fs.FS, options ...GeoTIFFTileSetOption) (*EUDEMElevationService, error) {
	geoTIFFFileSet, err := NewEUDEM(fsys, options...)
	if err != nil {
		return nil, err
	}
	pj, err := proj.NewCRSToCRS("epsg:4326", "epsg:3035", nil)
	if err != nil {
		return nil, err
	}
	return &EUDEMElevationService{
		geoTIFFFileSet: geoTIFFFileSet,
		pj:             pj,
	}, nil
}

func (s *EUDEMElevationService) Elevation(coords [][]float64) ([]float64, error) {
	return InterpolateBilinear(s.geoTIFFFileSet, coords)
}

func (s *EUDEMElevationService) Elevation4326(coords4326 [][]float64) ([]float64, error) {
	coords3035 := cloneCoords(coords4326)
	flipCoords(coords3035)
	if err := s.pj.ForwardFloat64Slices(coords3035); err != nil {
		return nil, err
	}
	flipCoords(coords3035)
	return s.Elevation(coords3035)
}

func cloneCoords(coords [][]float64) [][]float64 {
	clonedCoordsFlat := make([]float64, 2*len(coords))
	clonedCoords := make([][]float64, len(coords))
	for i, coord := range coords {
		copy(clonedCoordsFlat[2*i:2*i+2], coord)
		clonedCoords[i] = clonedCoordsFlat[2*i : 2*i+2]
	}
	return clonedCoords
}

func flipCoords(coords [][]float64) {
	for i, coord := range coords {
		coords[i][0], coords[i][1] = coord[1], coord[0]
	}
}
