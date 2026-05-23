package codec

import (
	"testing"
	"time"

	"gocv.io/x/gocv"
)

// TestTSEncoder_EntrypointShape verifies the entrypoint blob is tagged
// with CodecIDTS so the viewer constructs a TSDecoder. Pure parsing,
// no ffmpeg required.
func TestTSEncoder_EntrypointShape(t *testing.T) {
	enc := NewTSEncoder(TSEncoderConfig{Width: 640, Height: 480, FPS: 30})

	ep := enc.CreateEntrypoint(44100, 1, 30)
	if len(ep) != 15 {
		t.Fatalf("expected 15-byte entrypoint, got %d", len(ep))
	}

	sr, ch, fps, w, h, cid, ok := ParseH264Entrypoint(ep)
	if !ok {
		t.Fatal("entrypoint failed to parse")
	}
	if cid != CodecIDTS {
		t.Errorf("expected codec ID %d (TS), got %d", CodecIDTS, cid)
	}
	if sr != 44100 || ch != 1 || fps != 30 {
		t.Errorf("audio/fps mismatch: sr=%d ch=%d fps=%d", sr, ch, fps)
	}
	if w != 640 || h != 480 {
		t.Errorf("dimensions mismatch: %dx%d", w, h)
	}

	// ParseTSEntrypoint should accept TS but reject non-TS codec IDs.
	if _, _, _, _, _, ok := ParseTSEntrypoint(ep); !ok {
		t.Error("ParseTSEntrypoint rejected a valid TS entrypoint")
	}

	// Synthesize a non-TS (legacy raw-H.264) entrypoint and confirm
	// ParseTSEntrypoint rejects it. The H264 encoder no longer exposes
	// a CreateEntrypoint helper since we don't emit that format anymore.
	h264ep := append([]byte{}, ep...)
	h264ep[10] = CodecIDH264
	if _, _, _, _, _, ok := ParseTSEntrypoint(h264ep); ok {
		t.Error("ParseTSEntrypoint accepted a non-TS (H.264) entrypoint")
	}
}

// TestTSEncoder_RequiresStart confirms encode methods refuse to run
// before Start. Catches the most likely lifecycle misuse.
func TestTSEncoder_RequiresStart(t *testing.T) {
	enc := NewTSEncoder(TSEncoderConfig{Width: 320, Height: 240, FPS: 30})
	frame := gocv.NewMatWithSize(240, 320, gocv.MatTypeCV8UC3)
	defer frame.Close()

	if _, err := enc.EncodeVideo(frame, 0, 0); err == nil {
		t.Error("EncodeVideo before Start should error")
	}
	if _, err := enc.EncodeAudio(make([]byte, 16), 0, 0); err == nil {
		t.Error("EncodeAudio before Start should error")
	}
}

// TestTSCodec_VideoRoundtrip pushes synthesized frames through the
// encoder, feeds the muxed bytes into the decoder, and confirms the
// decoder eventually emits a decoded mat. Exercises the full
// H.264 → TS mux → TS demux → H.264 decode pipeline so PR1's
// per-NAL-PTS contract is observably working end-to-end.
func TestTSCodec_VideoRoundtrip(t *testing.T) {
	if !checkFFmpeg() {
		t.Skip("ffmpeg not available")
	}

	const (
		w, h    = 320, 240
		fps     = 30
		nFrames = 90 // 3 seconds; well past one GOP so an IDR lands
	)

	enc := NewTSEncoder(TSEncoderConfig{Width: w, Height: h, FPS: fps, Bitrate: "1M"})
	if err := enc.Start(); err != nil {
		t.Fatalf("encoder Start: %v", err)
	}
	defer enc.Close()

	dec := NewTSDecoder(TSDecoderConfig{Width: w, Height: h})
	if err := dec.Start(); err != nil {
		t.Fatalf("decoder Start: %v", err)
	}
	defer dec.Close()

	// Encode, then feed every muxer chunk back to the decoder.
	var muxedBytes int
	for i := 0; i < nFrames; i++ {
		frame := gocv.NewMatWithSize(h, w, gocv.MatTypeCV8UC3)
		// Gradient so frames differ frame-to-frame.
		frame.SetTo(gocv.NewScalar(float64(i*2%256), float64(i*3%256), float64(i*5%256), 0))

		ts := time.Duration(i) * time.Second / time.Duration(fps)
		muxed, err := enc.EncodeVideo(frame, ts, uint32(i))
		frame.Close()
		if err != nil {
			t.Fatalf("EncodeVideo[%d]: %v", i, err)
		}
		if len(muxed) > 0 {
			muxedBytes += len(muxed)
			dec.Decode(muxed)
		}
	}

	// Give ffmpeg's encoder/decoder pipelines time to flush.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		muxed, _ := enc.EncodeVideo(gocv.NewMatWithSize(h, w, gocv.MatTypeCV8UC3), 0, 0)
		if len(muxed) > 0 {
			muxedBytes += len(muxed)
			dec.Decode(muxed)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if muxedBytes == 0 {
		t.Fatal("encoder produced no muxed bytes")
	}

	// Drain the decoder for queued frames.
	var videoFrames int
	for {
		fr, _ := dec.Decode(nil)
		if fr == nil {
			break
		}
		if fr.Type == FrameTypeVideo && fr.VideoFrame != nil {
			fr.VideoFrame.Close()
			videoFrames++
		}
	}

	if videoFrames == 0 {
		t.Fatalf("roundtrip produced zero decoded video frames (muxed %d bytes)", muxedBytes)
	}
	t.Logf("roundtrip: %d frames in → %d frames out, %d bytes muxed",
		nFrames, videoFrames, muxedBytes)
}
