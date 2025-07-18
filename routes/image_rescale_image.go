package routes

import (
	"image"

	"github.com/nfnt/resize"
)

func rescaleImage(img image.Image, scale float64) (image.Image, error) {
	dX := img.Bounds().Dx()
	dY := img.Bounds().Dy()

	resized := resize.Resize(uint(float64(dX)*scale), uint(float64(dY)*scale), img, resize.Lanczos3)

	return resized, nil
}
