package codec

import (
	"testing"
	"time"

	"gocv.io/x/gocv"
)

func TestNewJPEGCodec(t *testing.T) {
	codec := NewJPEGCodec(90)

	if codec == nil {
		t.Fatal("expected non-nil codec")
	}
	if codec.quality != 90 {
		t.Errorf("expected quality 90, got %d", codec.quality)
	}
}

func TestChunkSize(t *testing.T) {
	if ChunkSize != 1048576 {
		t.Errorf("expected ChunkSize 1048576 (1MB), got %d", ChunkSize)
	}
}

func TestJPEGCodec_EncodeVideo(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Create a test frame
	frame := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	defer frame.Close()

	encoded, err := codec.EncodeVideo(frame, time.Second, 1)
	if err != nil {
		t.Fatalf("failed to encode video frame: %v", err)
	}

	// Check that encoded data has header + JPEG data
	if len(encoded) < FrameHeaderSize {
		t.Errorf("encoded data too small: %d bytes", len(encoded))
	}

	// Check frame marker
	if encoded[0] != 'V' || encoded[1] != 'I' || encoded[2] != 'D' || encoded[3] != 'F' {
		t.Error("missing video frame marker")
	}
}

func TestJPEGCodec_EncodeAudio(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Create test audio samples
	samples := make([]byte, 1024)
	for i := range samples {
		samples[i] = byte(i % 256)
	}

	encoded, err := codec.EncodeAudio(samples, time.Second, 1)
	if err != nil {
		t.Fatalf("failed to encode audio: %v", err)
	}

	// Check that encoded data has header + audio data
	if len(encoded) != FrameHeaderSize+len(samples) {
		t.Errorf("expected encoded size %d, got %d", FrameHeaderSize+len(samples), len(encoded))
	}

	// Check frame marker
	if encoded[0] != 'A' || encoded[1] != 'U' || encoded[2] != 'D' || encoded[3] != 'F' {
		t.Error("missing audio frame marker")
	}
}

func TestJPEGCodec_CreateEntrypoint(t *testing.T) {
	codec := NewJPEGCodec(85)

	entrypoint := codec.CreateEntrypoint(44100, 2, 30)

	if len(entrypoint) != 10 {
		t.Errorf("expected entrypoint size 10, got %d", len(entrypoint))
	}

	// Check marker
	if entrypoint[0] != 'E' || entrypoint[1] != 'N' || entrypoint[2] != 'T' || entrypoint[3] != 'R' {
		t.Error("missing entrypoint marker")
	}
}

func TestJPEGCodec_ParseEntrypoint(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Create and parse entrypoint
	entrypoint := codec.CreateEntrypoint(48000, 1, 60)

	sampleRate, channels, fps, valid := codec.ParseEntrypoint(entrypoint)
	if !valid {
		t.Fatal("expected valid entrypoint")
	}
	if sampleRate != 48000 {
		t.Errorf("expected sampleRate 48000, got %d", sampleRate)
	}
	if channels != 1 {
		t.Errorf("expected channels 1, got %d", channels)
	}
	if fps != 60 {
		t.Errorf("expected fps 60, got %d", fps)
	}
}

func TestJPEGCodec_ParseEntrypoint_Invalid(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Test with invalid data
	_, _, _, valid := codec.ParseEntrypoint([]byte{1, 2, 3})
	if valid {
		t.Error("expected invalid for short data")
	}

	_, _, _, valid = codec.ParseEntrypoint([]byte("NOTENTRYP"))
	if valid {
		t.Error("expected invalid for wrong marker")
	}
}

func TestJPEGCodec_Decode(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Create a test frame
	frame := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	defer frame.Close()

	// Encode then decode
	encoded, err := codec.EncodeVideo(frame, time.Second, 42)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	decoded, consumed := codec.Decode(encoded)
	if decoded == nil {
		t.Fatal("failed to decode")
	}
	defer decoded.VideoFrame.Close()

	if decoded.Type != FrameTypeVideo {
		t.Errorf("expected FrameTypeVideo, got %d", decoded.Type)
	}
	if decoded.Sequence != 42 {
		t.Errorf("expected sequence 42, got %d", decoded.Sequence)
	}
	if consumed != len(encoded) {
		t.Errorf("expected consumed %d, got %d", len(encoded), consumed)
	}
}

func TestFrameHeaderSize(t *testing.T) {
	if FrameHeaderSize != 20 {
		t.Errorf("expected FrameHeaderSize 20, got %d", FrameHeaderSize)
	}
}

func TestEncodeFrameHeaderWithTimestamp(t *testing.T) {
	header := EncodeFrameHeaderWithTimestamp(VideoFrameMarker, 1000, 123456789, 42)

	if len(header) != FrameHeaderSize {
		t.Errorf("expected header size %d, got %d", FrameHeaderSize, len(header))
	}

	// Check marker
	if header[0] != 'V' || header[1] != 'I' || header[2] != 'D' || header[3] != 'F' {
		t.Error("marker not encoded correctly")
	}
}

func TestJPEGQuality_AffectsSize(t *testing.T) {
	// Test that different quality settings produce different sizes
	frame := gocv.NewMatWithSize(200, 200, gocv.MatTypeCV8UC3)
	defer frame.Close()

	// Fill with some pattern
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			frame.SetUCharAt(y, x*3, uint8(x%256))
			frame.SetUCharAt(y, x*3+1, uint8(y%256))
			frame.SetUCharAt(y, x*3+2, 128)
		}
	}

	lowCodec := NewJPEGCodec(20)
	highCodec := NewJPEGCodec(95)

	lowEncoded, err := lowCodec.EncodeVideo(frame, 0, 0)
	if err != nil {
		t.Fatalf("failed to encode low quality: %v", err)
	}

	highEncoded, err := highCodec.EncodeVideo(frame, 0, 0)
	if err != nil {
		t.Fatalf("failed to encode high quality: %v", err)
	}

	if len(highEncoded) <= len(lowEncoded) {
		t.Errorf("expected high quality (%d) > low quality (%d)", len(highEncoded), len(lowEncoded))
	}
}
