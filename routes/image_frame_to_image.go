package routes

import (
	"fmt"
	"image"

	"github.com/asticode/go-astiav"
)

// frameToImageOptimized converts a frame to image with caching optimizations
func frameToImage(frame *astiav.Frame) (image.Image, error) {
	// Check frame dimensions to avoid processing invalid frames
	if frame.Width() <= 0 || frame.Height() <= 0 {
		return nil, fmt.Errorf("invalid frame dimensions: %dx%d", frame.Width(), frame.Height())
	}

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