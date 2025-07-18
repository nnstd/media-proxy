package routes

import (
	"bytes"
	"fmt"
	"image"
	"io"

	"image/gif"
	"image/jpeg"
	"image/png"

	"github.com/gen2brain/go-fitz"
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

	case "application/pdf",
		"application/epub+zip",
		"application/x-mobipocket-ebook",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation":
		doc, err := fitz.NewFromReader(r)
		if err != nil {
			return nil, err
		}

		defer doc.Close()

		if pageCount := doc.NumPage(); pageCount > 0 {
			return doc.Image(0)
		}

		return nil, fmt.Errorf("no pages found")

	default:
		return nil, fmt.Errorf("unsupported image format: %s", contentType)
	}
}

func readImageSlice(s []byte, contentType string) (image.Image, error) {
	return readImage(bytes.NewReader(s), contentType)
}
