package codec

import "gocv.io/x/gocv"

// FrameType indicates the type of decoded frame
type FrameType int

const (
	FrameTypeNone  FrameType = iota
	FrameTypeVideo           // Video frame (gocv.Mat)
	FrameTypeAudio           // Audio frame (PCM samples)
)

// DecodedFrame represents a decoded frame produced by a codec.Decoder.
type DecodedFrame struct {
	Type       FrameType
	VideoFrame *gocv.Mat // Set if Type == FrameTypeVideo
	AudioData  []byte    // Set if Type == FrameTypeAudio (16-bit PCM samples)
	Timestamp  uint64    // Nanoseconds since stream start
	Sequence   uint32    // Frame sequence number for ordering
}
