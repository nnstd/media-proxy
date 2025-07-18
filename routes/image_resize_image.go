package routes

import (
	"image"

	"github.com/nfnt/resize"
)

func resizeImage(img image.Image, width int, height int, interpolation resize.InterpolationFunction) (image.Image, error) {
	resized := resize.Resize(uint(width), uint(height), img, interpolation)

	return resized, nil
}
