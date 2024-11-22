package elevation

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestGeo_Parse(t *testing.T) {
	directory := []uint16{
		1, 1, 0, 22,
		1024, 0, 1, 1,
		1025, 0, 1, 1,
		1026, 34737, 28, 0,
		2048, 0, 1, 4258,
		2049, 34737, 86, 28,
		2050, 0, 1, 6258,
		2051, 0, 1, 8901,
		2054, 0, 1, 9102,
		2055, 34736, 1, 4,
		2056, 0, 1, 7019,
		2057, 34736, 1, 5,
		2059, 34736, 1, 6,
		2061, 34736, 1, 7,
		3072, 0, 1, 32767,
		3073, 34737, 400, 114,
		3074, 0, 1, 32767,
		3075, 0, 1, 10,
		3076, 0, 1, 9001,
		3082, 34736, 1, 2,
		3083, 34736, 1, 3,
		3088, 34736, 1, 1,
		3089, 34736, 1, 0,
	}
	doubleParams := []float64{
		52,
		10,
		4321000,
		3210000,
		0.0174532925199433,
		6378137, 298.257222101,
		0,
	}
	asciiParams := []byte("" +
		"PCS Name = ETRS89_ETRS_LAEA|" +
		"GCS Name = GCS_ETRS_1989|Datum = D_ETRS_1989|Ellipsoid = GRS_1980|Primem = Greenwich||" +
		"ESRI PE String = PROJCS[\"ETRS89_ETRS_LAEA\",GEOGCS[\"GCS_ETRS_1989\",DATUM[\"D_ETRS_1989\",SPHEROID[\"GRS_1980\",6378137.0,298.257222101]],PRIMEM[\"Greenwich\",0.0],UNIT[\"Degree\",0.0174532925199433]],PROJECTION[\"Lambert_Azimuthal_Equal_Area\"],PARAMETER[\"false_easting\",4321000.0],PARAMETER[\"false_northing\",3210000.0],PARAMETER[\"central_meridian\",10.0],PARAMETER[\"latitude_of_origin\",52.0],UNIT[\"Meter\",1.0]]|",
	)

	actual, err := ParseGeoKeys(directory, doubleParams, asciiParams)
	assert.NoError(t, err)

	assert.Equal(t, &ParsedGeoKeys{
		Params: map[GeoKey]int{
			GeoKeyGTModelType:   1,
			GeoKeyGTRasterType:  1,
			GeoKeyGeodeticCRS:   4258,
			GeoKeyGeodeticDatum: 6258,
			GeoKeyPrimeMeridian: 8901,
			GeoKeyAngularUnits:  9102,
			GeoKeyEllipsoid:     7019,
			GeoKeyProjection:    32767,
			GeoKeyProjMethod:    10,
			GeoKeyLinearUnits2:  9001,
			GeoKeyProjectedCRS:  32767,
		},
		DoubleParams: map[GeoKey]float64{
			GeoKeyGeogAngularUnitSize:                  0.0174532925199433,
			GeoKeyEllipsoidSemiMajorAxis:               6378137,
			GeoKeyEllipsoidInvFlattening:               298.257222101,
			GeoKeyPrimeMeridianLongitude:               0,
			GeoKeyFalseEastingProjLinearParameters:     4321000,
			GeoKeyFalseNorthingProjLinearParameters:    3210000,
			GeoKeyCenterLongitudeProjAngularParameters: 10,
			GeoKeyCenterLatitudeProjAngularParameters:  52,
		},
		ASCIIParams: map[GeoKey]string{
			GeoKeyGTCitation:   "PCS Name = ETRS89_ETRS_LAEA|",
			GeoKeyGeogCitation: "GCS Name = GCS_ETRS_1989|Datum = D_ETRS_1989|Ellipsoid = GRS_1980|Primem = Greenwich||",
			GeoKeyPCSCitation:  "ESRI PE String = PROJCS[\"ETRS89_ETRS_LAEA\",GEOGCS[\"GCS_ETRS_1989\",DATUM[\"D_ETRS_1989\",SPHEROID[\"GRS_1980\",6378137.0,298.257222101]],PRIMEM[\"Greenwich\",0.0],UNIT[\"Degree\",0.0174532925199433]],PROJECTION[\"Lambert_Azimuthal_Equal_Area\"],PARAMETER[\"false_easting\",4321000.0],PARAMETER[\"false_northing\",3210000.0],PARAMETER[\"central_meridian\",10.0],PARAMETER[\"latitude_of_origin\",52.0],UNIT[\"Meter\",1.0]]|",
		},
	}, actual)
}
