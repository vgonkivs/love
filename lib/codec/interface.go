package codec

import (
	"time"

	"gocv.io/x/gocv"
)

// Encoder encodes video and audio frames for streaming
type Encoder interface {
	// EncodeVideo encodes a video frame, returns encoded bytes with header
	EncodeVideo(frame gocv.Mat, timestamp time.Duration, sequence uint32) ([]byte, error)

	// EncodeAudio encodes audio samples, returns encoded bytes with header
	EncodeAudio(samples []byte, timestamp time.Duration, sequence uint32) ([]byte, error)

	// CreateEntrypoint creates the metadata blob for stream start
	CreateEntrypoint(sampleRate int, channels int, fps int) []byte

	// CreateStreamEnd creates the stream end notification blob
	CreateStreamEnd(totalDuration time.Duration, totalFrames uint32) []byte
}

// Decoder decodes multiplexed video and audio frames
type Decoder interface {
	// Decode parses a frame and returns its type and decoded frame
	Decode(data []byte) (*DecodedFrame, int)

	// ParseEntrypoint extracts metadata from entrypoint blob
	ParseEntrypoint(data []byte) (sampleRate int, channels int, fps int, valid bool)
}
