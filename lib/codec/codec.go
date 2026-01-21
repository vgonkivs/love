package codec

import (
	"encoding/binary"
)

const (
	// ChunkSize is the target size for each blob (2MB = ~8 seconds of A/V at 2Mbps)
	ChunkSize = 1974272 // 2MB
)

// Frame type markers for multiplexing audio and video
var (
	VideoFrameMarker = []byte{'V', 'I', 'D', 'F'} // Video frame marker
	AudioFrameMarker = []byte{'A', 'U', 'D', 'F'} // Audio frame marker
)

const (
	// FrameHeaderSize is the size of the frame header in bytes
	// 4 (marker) + 4 (size) + 8 (timestamp) + 4 (sequence) = 20 bytes
	FrameHeaderSize = 20
)

// EncodeFrameHeaderWithTimestamp creates a frame header with timestamp and sequence
func EncodeFrameHeaderWithTimestamp(frameType []byte, dataSize int, timestamp uint64, sequence uint32) []byte {
	header := make([]byte, FrameHeaderSize)
	copy(header[:4], frameType)
	binary.LittleEndian.PutUint32(header[4:8], uint32(dataSize))
	binary.LittleEndian.PutUint64(header[8:16], timestamp)
	binary.LittleEndian.PutUint32(header[16:20], sequence)
	return header
}
