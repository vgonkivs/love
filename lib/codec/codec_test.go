package codec

import (
	"github.com/huandu/go-assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"

	"gocv.io/x/gocv"
)

func TestNewJPEGCodec(t *testing.T) {
	codec := NewJPEGCodec(90)
	require.NotNil(t, codec)
	assert.Equal(t, codec.quality, 90)
}

func TestJPEGCodec_EncodeVideo(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Create a test frame
	frame := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	defer frame.Close()

	encoded, err := codec.EncodeVideo(frame, time.Second, 1)
	require.NoError(t, err)

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
	require.NoError(t, err)
	assert.Equal(t, len(encoded), FrameHeaderSize+len(samples))
	// Check frame marker
	if encoded[0] != 'A' || encoded[1] != 'U' || encoded[2] != 'D' || encoded[3] != 'F' {
		t.Error("missing audio frame marker")
	}
}

func TestJPEGCodec_CreateEntrypoint(t *testing.T) {
	codec := NewJPEGCodec(85)

	entrypoint := codec.CreateEntrypoint(44100, 2)
	require.NotNil(t, entrypoint)
	assert.Equal(t, len(entrypoint), 9)
	// Check marker
	if entrypoint[0] != 'E' || entrypoint[1] != 'N' || entrypoint[2] != 'T' || entrypoint[3] != 'R' {
		t.Error("missing entrypoint marker")
	}
}

func TestJPEGCodec_ParseEntrypoint(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Create and parse entrypoint
	entrypoint := codec.CreateEntrypoint(48000, 1)

	sampleRate, channels, err := codec.ParseEntrypoint(entrypoint)
	require.NoError(t, err)
	assert.Equal(t, sampleRate, 48000)
	assert.Equal(t, channels, 1)
}

func TestJPEGCodec_ParseEntrypoint_Invalid(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Test with invalid data
	_, _, err := codec.ParseEntrypoint([]byte{1, 2, 3})
	require.Error(t, err)

	_, _, err = codec.ParseEntrypoint([]byte("NOTENTRYP"))
	require.Error(t, err)
}

func TestJPEGCodec_Decode(t *testing.T) {
	codec := NewJPEGCodec(85)

	// Create a test frame
	frame := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	defer frame.Close()

	// Encode then decode
	encoded, err := codec.EncodeVideo(frame, time.Second, 42)
	require.NoError(t, err)

	decoded, consumed := codec.Decode(encoded)
	require.NotNil(t, decoded)

	defer decoded.VideoFrame.Close()
	assert.Equal(t, decoded.Type, FrameTypeVideo)
	assert.Equal(t, decoded.Sequence, uint32(42))
	assert.Equal(t, consumed, len(encoded))
}

func TestEncodeFrameHeaderWithTimestamp(t *testing.T) {
	header := EncodeFrameHeaderWithTimestamp(VideoFrameMarker, 1000, 123456789, 42)
	assert.Equal(t, len(header), FrameHeaderSize)
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
	require.NoError(t, err)

	highEncoded, err := highCodec.EncodeVideo(frame, 0, 0)
	require.NoError(t, err)
	require.Greater(t, len(highEncoded), len(lowEncoded))
}
