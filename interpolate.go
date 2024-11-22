package elevation

func InterpolateBilinear(raster Raster, coords [][]float64) ([]float64, error) {
	scaleX, scaleY := raster.Scale()
	rasterCoords := make([]Coord, 4*len(coords))
	for i, coord := range coords {
		x0 := scaleX * (int(coord[0]) / scaleX)
		y0 := scaleY * (int(coord[1]) / scaleY)
		x1 := x0 + scaleX
		y1 := y0 + scaleY
		rasterCoords[4*i+0] = Coord{X: x0, Y: y0}
		rasterCoords[4*i+1] = Coord{X: x1, Y: y0}
		rasterCoords[4*i+2] = Coord{X: x0, Y: y1}
		rasterCoords[4*i+3] = Coord{X: x1, Y: y1}
	}
	samples, err := raster.Samples(rasterCoords)
	if err != nil {
		return nil, err
	}
	result := make([]float64, len(coords))
	for i, coord := range coords {
		dx := (coord[0] - float64(scaleX*(int(coord[0])/scaleX))) / float64(scaleX)
		dy := (coord[1] - float64(scaleY*(int(coord[1])/scaleY))) / float64(scaleY)
		result[i] = 0 +
			samples[4*i+0]*(1-dx)*(1-dy) +
			samples[4*i+1]*dx*(1-dy) +
			samples[4*i+2]*(1-dx)*dy +
			samples[4*i+3]*dx*dy
	}
	return result, nil
}
