package routes

import (
	"fmt"
	"image"
	"log"
	"strconv"
	"strings"

	"github.com/asticode/go-astiav"
)

// extractFrameFromPosition extracts a frame from a specific position in the video with optimizations
// position can be: "first", "half", "last", or a time in seconds (e.g., "30.5")
//
// Optimizations implemented:
// - Early exit when target time tolerance is met
// - Skip expensive frame conversion for frames far from target
// - Reduced processing frequency for "last" position
// - Smart frame skipping based on time distance from target
func extractFrameFromPosition(urlStr string, position string) (image.Image, error) {
	// Open input format context
	inputFormatContext := astiav.AllocFormatContext()
	if inputFormatContext == nil {
		return nil, fmt.Errorf("failed to allocate format context")
	}
	defer inputFormatContext.Free()

	// Open input
	if err := inputFormatContext.OpenInput(urlStr, nil, nil); err != nil {
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

	// Optimization: For specific time targets, define tolerance for early exit
	const timeToleranceSeconds = 0.1 // Exit if we're within 100ms of target

	// Optimization: Track if we've passed the target time for early exit
	var passedTarget bool

	// Optimization: Track frame processing to reduce overhead
	var frameCount int64
	var lastProcessedTime float64

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

		// Calculate current frame time early to enable optimizations
		currentTime := float64(frame.Pts()) * float64(videoStream.TimeBase().Num()) / float64(videoStream.TimeBase().Den())
		frameCount++

		// Optimization: For specific time targets, skip frames that are far from target
		if targetTime > 0 {
			timeDiff := abs(currentTime - targetTime)

			// Skip expensive frame conversion if we're more than 5 seconds away from target
			if timeDiff > 5.0 && closestFrame == nil {
				continue
			}

			// Early exit if we've found a good enough frame and are moving away from target
			if closestFrame != nil && timeDiff > closestTimeDiff && timeDiff < timeToleranceSeconds {
				break
			}

			// Mark that we've passed the target time
			if currentTime > targetTime {
				passedTarget = true
			}
		}

		// Optimization: For "last" position, we can skip frame conversion for most frames
		// Only convert frames occasionally to update lastValidFrame, but always process the last few seconds
		if position == "last" {
			// Always process frames in the last 5 seconds, otherwise skip most frames
			videoDuration := float64(inputFormatContext.Duration()) / 1000000.0
			if videoDuration > 0 && currentTime < (videoDuration-5.0) {
				// Only process every 30th frame to reduce processing overhead
				if frameCount%30 != 0 {
					continue
				}
			}
		}

		// Optimization: For better performance, avoid processing frames too frequently when far from target
		if targetTime > 0 && lastProcessedTime > 0 {
			timeSinceLastProcess := abs(currentTime - lastProcessedTime)
			timeDiffFromTarget := abs(currentTime - targetTime)

			// If we're far from target and processed a frame recently, skip this one
			if timeDiffFromTarget > 2.0 && timeSinceLastProcess < 0.5 {
				continue
			}
		}

		lastProcessedTime = currentTime

		// Convert frame to image (only for frames we might actually need)
		img, err := frameToImage(frame)
		if err != nil {
			log.Printf("Failed to convert frame to image: %v, continuing...", err)
			continue
		}

		// Store the first valid frame we encounter
		if lastValidFrame == nil {
			lastValidFrame = img
		}

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

				// Optimization: Early exit for "half" position if we're very close
				if position == "half" && timeDiff < timeToleranceSeconds {
					break
				}
			}

			// Optimization: Early exit if we've passed target and have a good frame
			if passedTarget && closestFrame != nil && timeDiff > closestTimeDiff {
				break
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
		if duration <= 0 {
			// Fallback: estimate duration from stream duration if format duration is not available
			streamDuration := float64(videoStream.Duration()) * float64(videoStream.TimeBase().Num()) / float64(videoStream.TimeBase().Den())
			if streamDuration > 0 {
				duration = streamDuration
			} else {
				return 0, fmt.Errorf("unable to determine video duration for half position")
			}
		}
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
