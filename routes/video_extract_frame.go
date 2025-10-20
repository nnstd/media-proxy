package routes

import (
	"fmt"
	"image"
	"log"
	"strconv"
	"strings"

	"github.com/asticode/go-astiav"
)

// extractFrameFromPosition extracts a frame from a specific position in the video
// position can be: "first", "half", "last", or a time in seconds (e.g., "30.5")
func extractFrameFromPosition(urlStr string, position string) (image.Image, error) {
	// Open input format context
	inputFormatContext := astiav.AllocFormatContext()
	if inputFormatContext == nil {
		return nil, fmt.Errorf("failed to allocate format context")
	}
	defer inputFormatContext.Free()

	// Create dictionary for format options to improve MOV file handling
	formatOptions := astiav.NewDictionary()
	defer formatOptions.Free()

	// Increase analyzeduration and probesize for better codec detection (especially for MOV files)
	formatOptions.Set("analyzeduration", "100000000", 0) // 100 seconds in microseconds
	formatOptions.Set("probesize", "50000000", 0)        // 50MB

	// Open input
	if err := inputFormatContext.OpenInput(urlStr, nil, formatOptions); err != nil {
		return nil, fmt.Errorf("failed to open input: %w", err)
	}
	defer inputFormatContext.CloseInput()

	// Find stream info
	if err := inputFormatContext.FindStreamInfo(nil); err != nil {
		return nil, fmt.Errorf("failed to find stream info: %w", err)
	}

	// Find video stream
	videoStreamIndex := -1
	var videoStream *astiav.Stream
	for _, stream := range inputFormatContext.Streams() {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			videoStreamIndex = stream.Index()
			videoStream = stream
			break
		}
	}

	if videoStreamIndex == -1 {
		return nil, fmt.Errorf("no video stream found")
	}

	// Calculate target time based on position
	targetTime, err := calculateTargetTime(inputFormatContext, videoStream, position)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate target time: %w", err)
	}

	// Find decoder
	codec := astiav.FindDecoder(videoStream.CodecParameters().CodecID())
	if codec == nil {
		return nil, fmt.Errorf("failed to find decoder")
	}

	// Allocate codec context
	codecContext := astiav.AllocCodecContext(codec)
	if codecContext == nil {
		return nil, fmt.Errorf("failed to allocate codec context")
	}
	defer codecContext.Free()

	// Copy codec parameters
	if err := codecContext.FromCodecParameters(videoStream.CodecParameters()); err != nil {
		return nil, fmt.Errorf("failed to copy codec parameters: %w", err)
	}

	// Open codec
	if err := codecContext.Open(codec, nil); err != nil {
		return nil, fmt.Errorf("failed to open codec: %w", err)
	}

	// Note: Seeking is not implemented in this version of astiav
	// We'll read through the video to find the target frame

	// Allocate packet and frame
	packet := astiav.AllocPacket()
	defer packet.Free()

	frame := astiav.AllocFrame()
	defer frame.Free()

	var lastValidFrame image.Image
	var closestFrame image.Image
	var closestTimeDiff float64 = -1

	// Read frames until we find the target frame or reach the end
	for {
		if err := inputFormatContext.ReadFrame(packet); err != nil {
			if err == astiav.ErrEof {
				break
			}
			return nil, fmt.Errorf("failed to read frame: %w", err)
		}

		if packet.StreamIndex() != videoStreamIndex {
			packet.Unref()
			continue
		}

		// Send packet to decoder
		if err := codecContext.SendPacket(packet); err != nil {
			packet.Unref()
			return nil, fmt.Errorf("failed to send packet: %w", err)
		}
		packet.Unref()

		// Receive frame from decoder
		if err := codecContext.ReceiveFrame(frame); err != nil {
			if err == astiav.ErrEagain || err == astiav.ErrEof {
				continue
			}
			return nil, fmt.Errorf("failed to receive frame: %w", err)
		}

		// Check if frame has data before processing
		data, _ := frame.Data().Bytes(1)
		if len(data) == 0 {
			continue
		}

		// Convert frame to image
		img, err := frameToImage(frame)
		if err != nil {
			log.Printf("Failed to convert frame to image: %v, continuing...", err)
			continue
		}

		// Store the first valid frame we encounter
		if lastValidFrame == nil {
			lastValidFrame = img
		}

		// Calculate current frame time
		currentTime := float64(frame.Pts()) * float64(videoStream.TimeBase().Num()) / float64(videoStream.TimeBase().Den())

		// For "first" position, return immediately
		if position == "first" {
			return img, nil
		}

		// For "last" position, keep updating until we reach the end
		if position == "last" {
			lastValidFrame = img
			continue
		}

		// For specific time or "half", find the closest frame
		if targetTime > 0 {
			timeDiff := abs(currentTime - targetTime)
			if closestFrame == nil || timeDiff < closestTimeDiff {
				closestFrame = img
				closestTimeDiff = timeDiff
			}
		} else if targetTime == -1 {
			// For "last" position, keep updating the last valid frame
			lastValidFrame = img
		}
	}

	// Return appropriate frame based on position
	switch position {
	case "first":
		if lastValidFrame != nil {
			return lastValidFrame, nil
		}
	case "last":
		if lastValidFrame != nil {
			return lastValidFrame, nil
		}
	case "half":
		if closestFrame != nil {
			return closestFrame, nil
		}
		if lastValidFrame != nil {
			return lastValidFrame, nil
		}
	default:
		// For specific time
		if closestFrame != nil {
			return closestFrame, nil
		}
		if lastValidFrame != nil {
			return lastValidFrame, nil
		}
	}

	return nil, fmt.Errorf("no video frames found")
}

// calculateTargetTime calculates the target time in seconds based on the position parameter
func calculateTargetTime(inputFormatContext *astiav.FormatContext, videoStream *astiav.Stream, position string) (float64, error) {
	switch position {
	case "first":
		return 0, nil
	case "last":
		// For "last", we'll find the last frame by reading through the entire video
		return -1, nil // Special value to indicate we want the last frame
	case "half":
		// Calculate half of the video duration
		duration := float64(inputFormatContext.Duration()) / 1000000.0 // Duration is in microseconds
		return duration / 2, nil
	default:
		// Try to parse as a time in seconds
		if timeStr := strings.TrimSpace(position); timeStr != "" {
			if time, err := strconv.ParseFloat(timeStr, 64); err == nil && time >= 0 {
				return time, nil
			}
		}
		return 0, fmt.Errorf("invalid position: %s", position)
	}
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
