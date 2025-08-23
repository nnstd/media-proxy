package routes

import (
	"image"

	"github.com/nfnt/resize"
)

func resizeImage(img image.Image, width int, height int, interpolation resize.InterpolationFunction) (image.Image, error) {
	// If both width and height are specified, resize to exact dimensions
	if width > 0 && height > 0 {
		resized := resize.Resize(uint(width), uint(height), img, interpolation)
		return resized, nil
	}

	// If only width is specified, maintain aspect ratio
	if width > 0 && height == 0 {
		resized := resize.Resize(uint(width), 0, img, interpolation)
		return resized, nil
	}

	// If only height is specified, maintain aspect ratio
	if width == 0 && height > 0 {
		resized := resize.Resize(0, uint(height), img, interpolation)
		return resized, nil
	}

	// If neither width nor height is specified, return original image
	return img, nil
}
