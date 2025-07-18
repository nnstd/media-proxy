package routes

import (
	"fmt"
	"image"

	"github.com/asticode/go-astiav"
)

func frameToImage(frame *astiav.Frame) (image.Image, error) {
	img, err := frame.Data().GuessImageFormat()
	if err != nil {
		return nil, fmt.Errorf("failed to guess image format: %w", err)
	}

	err = frame.Data().ToImage(img)
	if err != nil {
		return nil, fmt.Errorf("failed to convert frame to image: %w", err)
	}

	return img, nil
}