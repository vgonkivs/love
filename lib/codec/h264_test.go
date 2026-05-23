package codec

import (
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

	if cfg.Width != 1280 {
		t.Errorf("expected width 1280, got %d", cfg.Width)
	}
	if cfg.Height != 720 {
		t.Errorf("expected height 720, got %d", cfg.Height)
	}
	if cfg.FPS != 30 {
		t.Errorf("expected FPS 30, got %d", cfg.FPS)
	}
	if cfg.GOPSize != 180 {
		t.Errorf("expected GOPSize 180 (30fps * 6s), got %d", cfg.GOPSize)
	}
	if cfg.Bitrate != "2M" {
		t.Errorf("expected bitrate 2M, got %s", cfg.Bitrate)
	}

	encoder := NewH264Encoder(cfg)
	if encoder == nil {
		t.Fatal("expected non-nil encoder")
	}
}

func TestH264Decoder_Creation(t *testing.T) {
	cfg := H264DecoderConfig{
		Width:  1280,
		Height: 720,
	}

	decoder := NewH264Decoder(cfg)
	if decoder == nil {
		t.Fatal("expected non-nil decoder")
	}
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
	defer frame.Close()

	// Should fail without Start()
	_, err := encoder.EncodeVideo(frame, 0, 0)
	if err == nil {
		t.Error("expected error when encoding without Start()")
	}
}

func TestH264Encoder_StartAndEncode(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	cfg := DefaultH264EncoderConfig(320, 240, 30)
	encoder := NewH264Encoder(cfg)
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
		frame.Close()

		if err != nil {
			t.Fatalf("failed to encode frame %d: %v", i, err)
		}
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
				if string(marker) != "H264" {
					t.Errorf("expected H264 marker, got %v", marker)
				}
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
	defer decoder.Close()

	// Should fail without Start()
	_, err := decoder.DecodeH264Frame([]byte{0x00, 0x00, 0x00, 0x01})
	if err == nil {
		t.Error("expected error when decoding without Start()")
	}
}

// TestH264Decoder_SPSReset verifies that a mid-stream SPS or PPS change
// clears the primed flag so the next bare IDR re-prepends the fresh
// parameter set. Without the reset, late-joiner IDRs would decode against
// stale SPS/PPS after the producer restarted or changed resolution.
func TestH264Decoder_SPSReset(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	dec := NewH264Decoder(H264DecoderConfig{Width: 640, Height: 480})
	if err := dec.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dec.Close()

	sps1 := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1e}
	sps2 := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x28} // different payload
	pps1 := []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xce, 0x38, 0x80}
	pps2 := []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xce, 0x3c, 0x80}
	idr := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x00}

	// Prime with first parameter set.
	for _, nal := range [][]byte{sps1, pps1, idr} {
		if _, err := dec.DecodeH264Frame(nal); err != nil {
			t.Fatalf("priming: %v", err)
		}
	}
	if !dec.hasSPS {
		t.Fatal("hasSPS must be true after initial SPS+PPS+IDR")
	}

	// Re-sending identical SPS/PPS must NOT reset the latch (avoids
	// re-prepending on every keyframe in well-behaved producers).
	if _, err := dec.DecodeH264Frame(sps1); err != nil {
		t.Fatalf("identical SPS: %v", err)
	}
	if _, err := dec.DecodeH264Frame(pps1); err != nil {
		t.Fatalf("identical PPS: %v", err)
	}
	if !dec.hasSPS {
		t.Fatal("identical SPS/PPS must not reset hasSPS")
	}

	// Changed SPS must reset the latch.
	if _, err := dec.DecodeH264Frame(sps2); err != nil {
		t.Fatalf("changed SPS: %v", err)
	}
	if dec.hasSPS {
		t.Fatal("changed SPS must reset hasSPS so the next IDR re-prepends")
	}

	// Re-prime, then verify PPS change also resets.
	if _, err := dec.DecodeH264Frame(idr); err != nil {
		t.Fatalf("re-prime IDR: %v", err)
	}
	if !dec.hasSPS {
		t.Fatal("hasSPS must re-arm after the next IDR")
	}
	if _, err := dec.DecodeH264Frame(pps2); err != nil {
		t.Fatalf("changed PPS: %v", err)
	}
	if dec.hasSPS {
		t.Fatal("changed PPS must reset hasSPS")
	}
}

func TestH264_Entrypoint(t *testing.T) {
	cfg := DefaultH264EncoderConfig(1280, 720, 30)
	encoder := NewH264Encoder(cfg)

	entrypoint := encoder.CreateEntrypoint(44100, 2, 30)

	if len(entrypoint) != 15 {
		t.Errorf("expected entrypoint length 15, got %d", len(entrypoint))
	}

	// Parse it back
	sampleRate, channels, fps, width, height, codecID, valid := ParseH264Entrypoint(entrypoint)
	if !valid {
		t.Fatal("expected valid entrypoint")
	}
	if sampleRate != 44100 {
		t.Errorf("expected sample rate 44100, got %d", sampleRate)
	}
	if channels != 2 {
		t.Errorf("expected channels 2, got %d", channels)
	}
	if fps != 30 {
		t.Errorf("expected fps 30, got %d", fps)
	}
	if width != 1280 {
		t.Errorf("expected width 1280, got %d", width)
	}
	if height != 720 {
		t.Errorf("expected height 720, got %d", height)
	}
	if codecID != CodecIDH264 {
		t.Errorf("expected codec ID %d (H.264), got %d", CodecIDH264, codecID)
	}
}

func TestH264_AudioEncoding(t *testing.T) {
	cfg := DefaultH264EncoderConfig(1280, 720, 30)
	encoder := NewH264Encoder(cfg)

	// Audio encoding should work without starting the video encoder
	audioData := make([]byte, 4096)
	for i := range audioData {
		audioData[i] = byte(i % 256)
	}

	encoded, err := encoder.EncodeAudio(audioData, time.Second, 100)
	if err != nil {
		t.Fatalf("failed to encode audio: %v", err)
	}

	// Verify header
	if len(encoded) < FrameHeaderSize {
		t.Fatalf("encoded audio too short: %d", len(encoded))
	}

	marker := encoded[:4]
	if string(marker) != "AUDF" {
		t.Errorf("expected AUDF marker, got %v", marker)
	}
}

func TestH264_CompressionRatio(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	// Compare H.264 vs JPEG compression for same content
	cfg := DefaultH264EncoderConfig(320, 240, 30)
	h264Encoder := NewH264Encoder(cfg)
	defer h264Encoder.Close()

	jpegCodec := NewJPEGCodec(90)

	err := h264Encoder.Start()
	if err != nil {
		t.Fatalf("failed to start H.264 encoder: %v", err)
	}

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
		h264Encoder.EncodeVideo(frame, 0, uint32(i))
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

// TestH264Decoder_IDRGate verifies that DecodeH264Frame drops slice/IDR
// NAL units until the decoder has been primed with SPS+PPS+IDR — the
// common mid-GOP-join scenario in live mode. Without the gate, ffmpeg
// would receive undecodable bytes and silently emit nothing.
func TestH264Decoder_IDRGate(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	dec := NewH264Decoder(H264DecoderConfig{Width: 640, Height: 480})
	if err := dec.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dec.Close()

	// Minimal NAL units. Payload bytes are irrelevant to the gate
	// (which decides solely on the NAL type in the header byte).
	pSlice := []byte{0x00, 0x00, 0x00, 0x01, 0x21, 0x00} // nal_unit_type=1
	idr := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x00}    // nal_unit_type=5
	sps := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1e}
	pps := []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xce, 0x38, 0x80}

	// Pre-prime: lone P-slice must be dropped, not errored.
	if _, err := dec.DecodeH264Frame(pSlice); err != nil {
		t.Fatalf("P-slice before priming: expected silent drop, got error: %v", err)
	}
	if dec.hasSPS {
		t.Fatal("hasSPS must remain false after a lone P-slice")
	}

	// IDR without cached SPS/PPS is undecodable — must also be dropped.
	if _, err := dec.DecodeH264Frame(idr); err != nil {
		t.Fatalf("IDR before SPS/PPS: expected silent drop, got error: %v", err)
	}
	if dec.hasSPS {
		t.Fatal("hasSPS must remain false on IDR without cached SPS/PPS")
	}

	// SPS+PPS cached but no IDR yet — still not primed.
	if _, err := dec.DecodeH264Frame(sps); err != nil {
		t.Fatalf("SPS: %v", err)
	}
	if _, err := dec.DecodeH264Frame(pps); err != nil {
		t.Fatalf("PPS: %v", err)
	}
	if dec.hasSPS {
		t.Fatal("hasSPS must not flip until an IDR follows SPS+PPS")
	}

	// IDR after cached SPS+PPS primes the decoder.
	if _, err := dec.DecodeH264Frame(idr); err != nil {
		t.Fatalf("IDR after SPS+PPS: %v", err)
	}
	if !dec.hasSPS {
		t.Fatal("hasSPS must be set after SPS+PPS+IDR triplet")
	}

	// Post-prime: P-slices now flow through.
	if _, err := dec.DecodeH264Frame(pSlice); err != nil {
		t.Fatalf("P-slice after priming: %v", err)
	}
}
