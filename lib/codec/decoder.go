package codec

import (
	"bytes"
	"encoding/binary"

	"gocv.io/x/gocv"
)

// JPEG markers
var (
	jpegSOI = []byte{0xFF, 0xD8} // Start of Image
	jpegEOI = []byte{0xFF, 0xD9} // End of Image
)

// FrameType indicates the type of decoded frame
type FrameType int

const (
	FrameTypeNone  FrameType = iota
	FrameTypeVideo           // Video frame (JPEG image)
	FrameTypeAudio           // Audio frame (PCM samples)
)

// DecodedFrame represents a decoded frame from the multiplexed stream
type DecodedFrame struct {
	Type       FrameType
	VideoFrame *gocv.Mat // Set if Type == FrameTypeVideo
	AudioData  []byte    // Set if Type == FrameTypeAudio (16-bit PCM samples)
	Timestamp  uint64    // Nanoseconds since stream start
	Sequence   uint32    // Frame sequence number for ordering
}

// DecodeNextFrame attempts to decode the next JPEG frame from the buffer (legacy format)
// Returns the decoded frame and the number of bytes consumed
// Returns nil, 0 if no complete frame is available
func DecodeNextFrame(data []byte) (*gocv.Mat, int) {
	if len(data) < 4 {
		return nil, 0
	}

	// Find JPEG start marker
	startIdx := bytes.Index(data, jpegSOI)
	if startIdx < 0 {
		return nil, 0
	}

	// Find JPEG end marker after start
	endIdx := bytes.Index(data[startIdx+2:], jpegEOI)
	if endIdx < 0 {
		return nil, 0
	}

	// Calculate actual end position (relative to full buffer)
	endPos := startIdx + 2 + endIdx + 2 // +2 for SOI skip, +2 for EOI length

	// Extract JPEG data
	jpegData := data[startIdx:endPos]

	// Decode JPEG
	img, err := gocv.IMDecode(jpegData, gocv.IMReadColor)
	if err != nil {
		// Invalid JPEG, skip to after SOI and try again
		return nil, startIdx + 2
	}

	if img.Empty() {
		// Failed to decode, skip this marker
		return nil, startIdx + 2
	}

	return &img, endPos
}

// DecodeNextMultiplexedFrame decodes the next frame from a multiplexed audio/video stream
// Returns the decoded frame type and data, plus bytes consumed
// Returns FrameTypeNone if no complete frame is available
// Supports both legacy 8-byte header and new 20-byte header with timestamps
func DecodeNextMultiplexedFrame(data []byte) (*DecodedFrame, int) {
	// Need at least 8 bytes for legacy header (4 type + 4 size)
	if len(data) < 8 {
		return nil, 0
	}

	// Check for frame type marker
	marker := data[:4]
	isVideo := bytes.Equal(marker, VideoFrameMarker)
	isAudio := bytes.Equal(marker, AudioFrameMarker)

	if !isVideo && !isAudio {
		// Unknown frame type - skip one byte and try again
		return nil, 1
	}

	frameSize := int(binary.LittleEndian.Uint32(data[4:8]))

	// Determine header size: check if we have timestamp data
	// New format: 20 bytes (4 type + 4 size + 8 timestamp + 4 sequence)
	// Legacy format: 8 bytes (4 type + 4 size)
	var headerSize int
	var timestamp uint64
	var sequence uint32

	// Try new format first (20 bytes header)
	if len(data) >= FrameHeaderSize {
		// Check if the frame size makes sense with 20-byte header
		if FrameHeaderSize+frameSize <= len(data) {
			headerSize = FrameHeaderSize
			timestamp = binary.LittleEndian.Uint64(data[8:16])
			sequence = binary.LittleEndian.Uint32(data[16:20])
		} else if 8+frameSize <= len(data) {
			// Fall back to legacy 8-byte header
			headerSize = 8
		} else {
			return nil, 0 // Not enough data
		}
	} else if 8+frameSize <= len(data) {
		// Legacy format
		headerSize = 8
	} else {
		return nil, 0 // Not enough data
	}

	totalSize := headerSize + frameSize
	if len(data) < totalSize {
		return nil, 0
	}

	frameData := data[headerSize:totalSize]

	if isVideo {
		// Decode JPEG
		img, err := gocv.IMDecode(frameData, gocv.IMReadColor)
		if err != nil || img.Empty() {
			// Invalid frame, skip it
			return nil, totalSize
		}
		return &DecodedFrame{
			Type:       FrameTypeVideo,
			VideoFrame: &img,
			Timestamp:  timestamp,
			Sequence:   sequence,
		}, totalSize
	}

	if isAudio {
		// Copy audio data
		audioCopy := make([]byte, len(frameData))
		copy(audioCopy, frameData)
		return &DecodedFrame{
			Type:      FrameTypeAudio,
			AudioData: audioCopy,
			Timestamp: timestamp,
			Sequence:  sequence,
		}, totalSize
	}

	return nil, 1
}
