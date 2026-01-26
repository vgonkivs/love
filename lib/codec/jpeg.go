package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"gocv.io/x/gocv"
)

// JPEGCodec implements Encoder and Decoder for JPEG video with PCM audio
type JPEGCodec struct {
	quality int
}

// NewJPEGCodec creates a new JPEG codec with the specified quality (1-100)
func NewJPEGCodec(quality int) *JPEGCodec {
	return &JPEGCodec{
		quality: quality,
	}
}

// EncodeVideo encodes a video frame to JPEG with header
func (c *JPEGCodec) EncodeVideo(frame gocv.Mat, timestamp time.Duration, sequence uint32) ([]byte, error) {
	buf, err := gocv.IMEncodeWithParams(".jpg", frame, []int{gocv.IMWriteJpegQuality, c.quality})
	if err != nil {
		return nil, err
	}
	defer buf.Close()

	frameData := buf.GetBytes()
	header := EncodeFrameHeaderWithTimestamp(VideoFrameMarker, len(frameData), uint64(timestamp.Nanoseconds()), sequence)

	result := make([]byte, len(header)+len(frameData))
	copy(result, header)
	copy(result[len(header):], frameData)

	return result, nil
}

// EncodeAudio encodes audio samples with header
func (c *JPEGCodec) EncodeAudio(samples []byte, timestamp time.Duration, sequence uint32) ([]byte, error) {
	header := EncodeFrameHeaderWithTimestamp(AudioFrameMarker, len(samples), uint64(timestamp.Nanoseconds()), sequence)

	result := make([]byte, len(header)+len(samples))
	copy(result, header)
	copy(result[len(header):], samples)

	return result, nil
}

// CreateEntrypoint creates the metadata blob for stream start
// Format: ENTR (4 bytes) + sample_rate (4 bytes) + channels (1 byte)
func (c *JPEGCodec) CreateEntrypoint(sampleRate int, channels int) []byte {
	data := make([]byte, 9)
	copy(data[:4], EntrypointMarker)
	binary.LittleEndian.PutUint32(data[4:8], uint32(sampleRate))
	data[8] = byte(channels)
	return data
}

// Decode parses the next frame from multiplexed data
// Returns the decoded frame and bytes consumed
func (c *JPEGCodec) Decode(data []byte) (*DecodedFrame, int) {
	return DecodeNextMultiplexedFrame(data)
}

// ParseEntrypoint extracts metadata from entrypoint blob
func (c *JPEGCodec) ParseEntrypoint(data []byte) (sampleRate int, channels int, err error) {
	if len(data) < 9 {
		return 0, 0, fmt.Errorf("invalid JPEG entrypoint frame")
	}
	if !bytes.Equal(data[:4], EntrypointMarker) {
		return 0, 0, fmt.Errorf("not an entrypoint blob")
	}
	sampleRate = int(binary.LittleEndian.Uint32(data[4:8]))
	channels = int(data[8])
	return sampleRate, channels, nil
}

// EntrypointMarker for entrypoint blobs
var EntrypointMarker = []byte{'E', 'N', 'T', 'R'}

// StreamEndMarker for stream end notification
var StreamEndMarker = []byte{'E', 'N', 'D', 'S'}

// CreateStreamEnd creates the stream end notification blob
// Format: ENDS (4 bytes) + total_duration_ns (8 bytes) + total_frames (4 bytes)
func (c *JPEGCodec) CreateStreamEnd(totalDuration time.Duration, totalFrames uint32) []byte {
	data := make([]byte, 16)
	copy(data[:4], StreamEndMarker)
	binary.LittleEndian.PutUint64(data[4:12], uint64(totalDuration.Nanoseconds()))
	binary.LittleEndian.PutUint32(data[12:16], totalFrames)
	return data
}
