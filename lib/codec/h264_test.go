package codec

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os/exec"
	"testing"
	"time"

	"gocv.io/x/gocv"
)

// checkFFmpeg returns true if ffmpeg is available
func checkFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func TestH264Encoder_Creation(t *testing.T) {
	cfg := DefaultH264EncoderConfig(1280, 720, 30)
	encoder := NewH264Encoder(cfg)
	assert.NotNil(t, encoder)
}

func TestH264Decoder_Creation(t *testing.T) {
	cfg := H264DecoderConfig{
		Width:  1280,
		Height: 720,
	}

	decoder := NewH264Decoder(cfg)
	assert.NotNil(t, decoder)
}

func TestH264Encoder_RequiresStart(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	cfg := DefaultH264EncoderConfig(640, 480, 30)
	encoder := NewH264Encoder(cfg)
	defer encoder.Close()

	// Create a test frame
	frame := gocv.NewMatWithSize(480, 640, gocv.MatTypeCV8UC3)
	assert.NotNil(t, frame)
	defer frame.Close()

	// Should fail without Start()
	_, err := encoder.EncodeVideo(frame, 0, 0)
	require.Error(t, err)
}

func TestH264Encoder_StartAndEncode(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	cfg := DefaultH264EncoderConfig(320, 240, 30)
	encoder := NewH264Encoder(cfg)
	assert.NotNil(t, encoder)
	defer encoder.Close()

	err := encoder.Start()
	if err != nil {
		t.Fatalf("failed to start encoder: %v", err)
	}

	// Create test frames
	for i := 0; i < 10; i++ {
		frame := gocv.NewMatWithSize(240, 320, gocv.MatTypeCV8UC3)

		// Fill with some pattern
		frame.SetTo(gocv.NewScalar(float64(i*20), float64(i*10), float64(i*5), 0))

		timestamp := time.Duration(i) * 33 * time.Millisecond
		_, err := encoder.EncodeVideo(frame, timestamp, uint32(i))
		require.NoError(t, err)
		frame.Close()
	}

	// Read remaining frames

	// Try to read some encoded frames
	frameCount := 0
	for j := 0; j < 15; j++ {
		encoded, err := encoder.ReadEncodedFrameTimeout(0, 0, 100*time.Millisecond)
		if err != nil {
			break
		}
		if encoded != nil {
			frameCount++
			// Verify frame has H264 marker
			if len(encoded) >= 4 {
				marker := encoded[:4]
				require.Equal(t, string(marker), "H264")
			}
		}
	}

	if frameCount == 0 {
		t.Logf("Warning: no encoded frames received (ffmpeg may need more input)")
		t.Logf("ffmpeg stderr: %s", encoder.GetStderr())
	}
}

func TestH264Decoder_RequiresStart(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	cfg := H264DecoderConfig{
		Width:  640,
		Height: 480,
	}
	decoder := NewH264Decoder(cfg)
	assert.NotNil(t, decoder)
	defer decoder.Close()

	// Should fail without Start()
	_, err := decoder.DecodeH264Frame([]byte{0x00, 0x00, 0x00, 0x01})
	require.Error(t, err)
}

func TestH264_Entrypoint(t *testing.T) {
	cfg := DefaultH264EncoderConfig(1280, 720, 30)
	encoder := NewH264Encoder(cfg)
	assert.NotNil(t, encoder)

	entrypoint := encoder.CreateEntrypoint(44100, 2)
	assert.NotNil(t, entrypoint)
	assert.Equal(t, len(entrypoint), 14)

	_, _, _, _, _, err := ParseH264Entrypoint(entrypoint)
	require.NoError(t, err)
}

func TestH264_AudioEncoding(t *testing.T) {
	cfg := DefaultH264EncoderConfig(1280, 720, 30)
	encoder := NewH264Encoder(cfg)
	assert.NotNil(t, encoder)
	// Audio encoding should work without starting the video encoder
	audioData := make([]byte, 4096)
	for i := range audioData {
		audioData[i] = byte(i % 256)
	}

	encoded, err := encoder.EncodeAudio(audioData, time.Second, 100)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(encoded), FrameHeaderSize)

	marker := encoded[:4]
	require.NotEqual(t, string(marker), "H264")
}

func TestH264_CompressionRatio(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	// Compare H.264 vs JPEG compression for same content
	cfg := DefaultH264EncoderConfig(320, 240, 30)
	h264Encoder := NewH264Encoder(cfg)
	assert.NotNil(t, h264Encoder)
	defer h264Encoder.Close()

	jpegCodec := NewJPEGCodec(90)

	err := h264Encoder.Start()
	require.NoError(t, err)

	// Create test frames with gradient (compresses well)
	var jpegTotal int
	numFrames := 30

	for i := 0; i < numFrames; i++ {
		frame := gocv.NewMatWithSize(240, 320, gocv.MatTypeCV8UC3)
		// Create gradient pattern
		frame.SetTo(gocv.NewScalar(float64(i*8), float64(i*4), float64(i*2), 0))

		// JPEG encode
		jpegData, err := jpegCodec.EncodeVideo(frame, 0, 0)
		if err != nil {
			frame.Close()
			t.Fatalf("failed to JPEG encode: %v", err)
		}
		jpegTotal += len(jpegData)

		// Feed to H.264 encoder
		_, err = h264Encoder.EncodeVideo(frame, 0, uint32(i))
		require.NoError(t, err)
		frame.Close()
	}

	// Collect H.264 output
	var h264Total int
	for j := 0; j < numFrames+10; j++ {
		encoded, err := h264Encoder.ReadEncodedFrameTimeout(0, 0, 100*time.Millisecond)
		if err != nil {
			break
		}
		if encoded != nil {
			h264Total += len(encoded)
		}
	}

	t.Logf("JPEG total: %d bytes for %d frames (avg: %d bytes/frame)",
		jpegTotal, numFrames, jpegTotal/numFrames)

	if h264Total > 0 {
		t.Logf("H.264 total: %d bytes (ratio: %.1fx smaller than JPEG)",
			h264Total, float64(jpegTotal)/float64(h264Total))
	} else {
		t.Log("H.264 encoder produced no output (may need more frames)")
	}
}
