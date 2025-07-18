package main

import (
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/nfnt/resize"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"

	"github.com/kolesa-team/go-webp/decoder"
	"github.com/kolesa-team/go-webp/webp"
)

func readImage(r io.Reader, contentType string) (image.Image, error) {
	switch contentType {
	case "image/jpeg":
		return jpeg.Decode(r)

	case "image/png":
		return png.Decode(r)

	case "image/gif":
		return gif.Decode(r)

	case "image/bmp":
		return bmp.Decode(r)

	case "image/tiff":
		return tiff.Decode(r)

	case "image/webp":
		return webp.Decode(r, &decoder.Options{})

	default:
		return nil, fmt.Errorf("unsupported image format: %s", contentType)
	}
}

func resizeImage(img image.Image, width int, height int) (image.Image, error) {
	resized := resize.Resize(uint(width), uint(height), img, resize.Lanczos3)

	return resized, nil
}

func rescaleImage(img image.Image, scale float64) (image.Image, error) {
	dX := img.Bounds().Dx()
	dY := img.Bounds().Dy()

	resized := resize.Resize(uint(float64(dX)*scale), uint(float64(dY)*scale), img, resize.Lanczos3)

	return resized, nil
}
