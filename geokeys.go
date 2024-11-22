package elevation

import "errors"

var errParse = errors.New("parse error")

type GeoKey uint16

const (
	GeoKeyGTModelType  GeoKey = 1024
	GeoKeyGTRasterType GeoKey = 1025
	GeoKeyGTCitation   GeoKey = 1026

	GeoKeyGeodeticCRS            GeoKey = 2048
	GeoKeyGeogCitation           GeoKey = 2049
	GeoKeyGeodeticDatum          GeoKey = 2050
	GeoKeyPrimeMeridian          GeoKey = 2051
	GeoKeyLinearUnits            GeoKey = 2052
	GeoKeyGeogLinearUnitSize     GeoKey = 2053
	GeoKeyAngularUnits           GeoKey = 2054
	GeoKeyGeogAngularUnitSize    GeoKey = 2055
	GeoKeyEllipsoid              GeoKey = 2056
	GeoKeyEllipsoidSemiMajorAxis GeoKey = 2057
	GeoKeyEllipsoidSemiMinorAxis GeoKey = 2058
	GeoKeyEllipsoidInvFlattening GeoKey = 2059
	GeoKeyAzimuthUnits           GeoKey = 2060
	GeoKeyPrimeMeridianLongitude GeoKey = 2061

	GeoKeyProjectedCRS                                 GeoKey = 3072
	GeoKeyPCSCitation                                  GeoKey = 3073
	GeoKeyProjection                                   GeoKey = 3074
	GeoKeyProjMethod                                   GeoKey = 3075
	GeoKeyLinearUnits2                                 GeoKey = 3076
	GeoKeyProjectedLinearUnitSize                      GeoKey = 3077
	GeoKeyStandardParallel1GeoKeyProjAngularParameters GeoKey = 3078
	GeoKeyStandardParallel2GeoKeyProjAngularParameters GeoKey = 3079
	GeoKeyNaturalOriginLongitudeProjAngularParameters  GeoKey = 3080
	GeoKeyNaturalOriginLatitudeProjAngularParameters   GeoKey = 3081
	GeoKeyFalseEastingProjLinearParameters             GeoKey = 3082
	GeoKeyFalseNorthingProjLinearParameters            GeoKey = 3083
	GeoKeyFalseOriginLongitudeProjAngularParameters    GeoKey = 3084
	GeoKeyFalseOriginLatitudeProjAngularParameters     GeoKey = 3085
	GeoKeyFalseOriginEastingProjLinearParameters       GeoKey = 3086
	GeoKeyFalseOriginNorthingProjLinearParameters      GeoKey = 3087
	GeoKeyCenterLongitudeProjAngularParameters         GeoKey = 3088
	GeoKeyCenterLatitudeProjAngularParameters          GeoKey = 3089
	GeoKeyProjectionCenterEastingProjLinearParameters  GeoKey = 3090
	GeoKeyProjectionCenterNorthingProjLinearParameters GeoKey = 3091
	GeoKeyScaleAtNaturalOriginProjScalarParameters     GeoKey = 3092
	GeoKeyScaleAtCenterProjScalarParameters            GeoKey = 3093
	GeoKeyProjAzimuthAngle                             GeoKey = 3094
	GeoKeyStraightVerticalPoleProjAngularParameters    GeoKey = 3095

	GeoKeyVertical         GeoKey = 4096
	GeoKeyVerticalCitation GeoKey = 4097
	GeoKeyVerticalDatum    GeoKey = 4098
	GeoKeyVerticalUnits    GeoKey = 4099
)

type ParsedGeoKeys struct {
	Params       map[GeoKey]int
	DoubleParams map[GeoKey]float64
	ASCIIParams  map[GeoKey]string
}

func ParseGeoKeys(directory []uint16, doubleParams []float64, asciiParams []byte) (*ParsedGeoKeys, error) {
	if len(directory) < 4 {
		return nil, errParse
	}

	if keyDirectoryVersion := int(directory[0]); keyDirectoryVersion != 1 {
		return nil, errParse
	}
	if keyRevision := int(directory[1]); keyRevision != 1 {
		return nil, errParse
	}
	if minorRevision := int(directory[2]); minorRevision != 0 && minorRevision != 1 {
		return nil, errParse
	}
	numberOfKeys := int(directory[3])
	if len(directory) != 4+4*numberOfKeys {
		return nil, errParse
	}

	parsedGeoKeys := &ParsedGeoKeys{
		Params:       make(map[GeoKey]int),
		DoubleParams: make(map[GeoKey]float64),
		ASCIIParams:  make(map[GeoKey]string),
	}
	for i := range numberOfKeys {
		keyValues := directory[4+4*i : 4+4*(i+1)]
		key := GeoKey(keyValues[0])
		tiffTagLocation := int(keyValues[1])
		numberOfValues := int(keyValues[2])
		switch tiffTagLocation {
		case 0:
			if numberOfValues != 1 {
				return nil, errParse
			}
			parsedGeoKeys.Params[key] = int(keyValues[3])
		case 34736: // GeoDoubleParamsTag
			index := int(keyValues[3])
			if numberOfValues != 1 {
				return nil, errors.ErrUnsupported
			}
			parsedGeoKeys.DoubleParams[key] = doubleParams[index]
		case 34737: // GeoASCIIParamsTag
			index := int(keyValues[3])
			parsedGeoKeys.ASCIIParams[key] = string(asciiParams[index : index+numberOfValues])
		default:
			return nil, errors.ErrUnsupported
		}
	}
	return parsedGeoKeys, nil
}
