package routes

import (
	"fmt"
	"image"
	"io"

	"image/gif"
	"image/jpeg"
	"image/png"

	"github.com/kolesa-team/go-webp/decoder"
	"github.com/kolesa-team/go-webp/webp"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
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
