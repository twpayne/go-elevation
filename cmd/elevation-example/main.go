package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/twpayne/go-elevation"
)

func run() error {
	euDEM := flag.String("eu_dem-path", os.Getenv("EU_DEM_PATH"), "path to EU DEM data")
	flag.Parse()

	if flag.NArg() != 2 {
		return errors.New("syntax: elevation-example latitude longitude")
	}
	lat, err := strconv.ParseFloat(flag.Arg(0), 64)
	if err != nil {
		return err
	}
	lon, err := strconv.ParseFloat(flag.Arg(1), 64)
	if err != nil {
		return err
	}

	es, err := elevation.NewEUDEMElevationService(
		os.DirFS(*euDEM),
		elevation.WithCanaryFilename("eu_dem_v11_E40N30.TIF"),
	)
	if err != nil {
		return err
	}

	coords := [][]float64{{lon, lat}}
	elevations, err := es.Elevation4326(coords)
	if err != nil {
		return nil
	}
	fmt.Println(elevations[0])

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
