package main

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"log"

	"github.com/asticode/go-astiav"
)

func extractFirstFrame(urlStr string) ([]byte, error) {
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

	// Allocate packet and frame
	packet := astiav.AllocPacket()
	defer packet.Free()

	frame := astiav.AllocFrame()
	defer frame.Free()

	// Read frames until we get the first video frame with data
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

		// Encode image to JPEG
		buf := new(bytes.Buffer)
		if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 85}); err != nil {
			log.Printf("Failed to encode JPEG: %v, continuing...", err)
			continue
		}

		return buf.Bytes(), nil
	}

	return nil, fmt.Errorf("no video frames found")
}