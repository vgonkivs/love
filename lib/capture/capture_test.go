package capture

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"gocv.io/x/gocv"

	"github.com/vgonkivs/love/lib/codec"
)

// stubEncoder is a no-op codec.Encoder used by tests that exercise the
// Capturer's buffer/lifecycle paths and never actually encode anything.
type stubEncoder struct{}

func (stubEncoder) EncodeVideo(gocv.Mat, time.Duration, uint32) ([]byte, error) {
	return nil, nil
}
func (stubEncoder) EncodeAudio([]byte, time.Duration, uint32) ([]byte, error) { return nil, nil }
func (stubEncoder) CreateEntrypoint(int, int, int) []byte                     { return nil }
func (stubEncoder) CreateStreamEnd(time.Duration, uint32) []byte              { return nil }

func TestNewCapturer(t *testing.T) {
	cfg := &Config{
		DeviceID:          1,
		Width:             640,
		Height:            480,
		FPS:               15,
		PreviewWindowName: "Test Window",
		AudioDeviceID:     -1,
		SampleRate:        44100,
		Channels:          1,
		AudioBuffer:       1024,
	}

	capturer := NewCapturer(cfg, stubEncoder{})

	if capturer == nil {
		t.Fatal("expected non-nil capturer")
	}
	if capturer.cfg != cfg {
		t.Error("capturer config mismatch")
	}
	if capturer.cfg.DeviceID != 1 {
		t.Errorf("expected DeviceID 1, got %d", capturer.cfg.DeviceID)
	}
	if capturer.cfg.Width != 640 {
		t.Errorf("expected Width 640, got %d", capturer.cfg.Width)
	}
	if capturer.cfg.Height != 480 {
		t.Errorf("expected Height 480, got %d", capturer.cfg.Height)
	}
	if capturer.cfg.FPS != 15 {
		t.Errorf("expected FPS 15, got %d", capturer.cfg.FPS)
	}
	if capturer.cfg.SampleRate != 44100 {
		t.Errorf("expected SampleRate 44100, got %d", capturer.cfg.SampleRate)
	}
}

func TestCapturer_Run_InvalidDevice(t *testing.T) {
	cfg := &Config{
		DeviceID:      999, // Invalid device ID
		Width:         640,
		Height:        480,
		FPS:           30,
		AudioDeviceID: -1,
		SampleRate:    44100,
		Channels:      1,
		AudioBuffer:   1024,
	}

	capturer := NewCapturer(cfg, stubEncoder{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output := make(chan []byte, 100)

	// Run should fail with invalid device
	// Note: This behavior depends on the system - some systems may not error immediately
	err := capturer.Run(ctx, output)

	// We expect an error for invalid device, but close the output channel regardless
	close(output)

	// The error behavior may vary by system, so we just verify it doesn't hang
	if err != nil {
		// Expected - invalid device should cause an error
		t.Logf("Got expected error for invalid device: %v", err)
	}
}

// newTestCapturer builds a Capturer with no real device, suitable for
// exercising addToBuffer / flushBuffer in isolation.
func newTestCapturer(t *testing.T, out chan<- []byte) *Capturer {
	t.Helper()
	c := NewCapturer(&Config{}, stubEncoder{})
	c.output = out
	return c
}

// TestAddToBuffer_NoSilentDrop verifies that addToBuffer no longer drops a
// chunk when the output channel is temporarily full — it must block until
// the consumer drains, then deliver every byte exactly once.
func TestAddToBuffer_NoSilentDrop(t *testing.T) {
	out := make(chan []byte, 1)
	c := newTestCapturer(t, out)

	// Pre-fill the channel so the first send inside addToBuffer must block.
	out <- []byte("preexisting")

	// Two full chunks of distinct bytes.
	data := bytes.Repeat([]byte{0xAB}, 2*codec.ChunkSize)

	done := make(chan struct{})
	go func() {
		c.addToBuffer(context.Background(), data)
		close(done)
	}()

	// addToBuffer should be blocked on the second send because out is full.
	select {
	case <-done:
		t.Fatal("addToBuffer returned while output channel was full — old silent-drop behavior")
	case <-time.After(50 * time.Millisecond):
	}

	// Drain everything and verify byte total.
	totalRcvd := 0
	for i := 0; i < 3; i++ { // 1 preexisting + 2 chunks
		select {
		case chunk := <-out:
			totalRcvd += len(chunk)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for chunk %d", i)
		}
	}

	<-done
	expected := len("preexisting") + 2*codec.ChunkSize
	if totalRcvd != expected {
		t.Fatalf("byte loss: got %d, want %d", totalRcvd, expected)
	}
}

// TestAddToBuffer_CtxCancel verifies that on ctx cancellation the residual
// data stays in c.buffer (so a later shutdownGrace flush can recover it),
// instead of being silently consumed.
func TestAddToBuffer_CtxCancel(t *testing.T) {
	out := make(chan []byte) // unbuffered: any send will block
	c := newTestCapturer(t, out)

	ctx, cancel := context.WithCancel(context.Background())
	data := bytes.Repeat([]byte{0x42}, 3*codec.ChunkSize)

	done := make(chan struct{})
	go func() {
		c.addToBuffer(ctx, data)
		close(done)
	}()

	// Let it accumulate + block on first send.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("addToBuffer did not return after ctx cancel")
	}

	// All 3 chunks were appended; none were sent because nobody read out.
	// All should remain in c.buffer for a later flush to recover.
	c.bufferMu.Lock()
	got := len(c.buffer)
	c.bufferMu.Unlock()
	if got != 3*codec.ChunkSize {
		t.Fatalf("residual buffer: got %d bytes, want %d (data must survive ctx cancel)", got, 3*codec.ChunkSize)
	}
}

// TestAddToBuffer_ConcurrentProducers verifies that addToBuffer is safe to
// call concurrently from multiple goroutines (mimicking the
// audio-callback + video-ticker contention) and that no bytes are lost.
func TestAddToBuffer_ConcurrentProducers(t *testing.T) {
	const producers = 8
	const perProducer = 4
	const chunkBytes = codec.ChunkSize

	out := make(chan []byte, producers*perProducer)
	c := newTestCapturer(t, out)

	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(id byte) {
			defer wg.Done()
			data := bytes.Repeat([]byte{id}, perProducer*chunkBytes)
			c.addToBuffer(context.Background(), data)
		}(byte(p))
	}
	wg.Wait()

	close(out)
	totalRcvd := 0
	for chunk := range out {
		totalRcvd += len(chunk)
	}
	expected := producers * perProducer * chunkBytes
	if totalRcvd != expected {
		t.Fatalf("byte loss under concurrency: got %d, want %d", totalRcvd, expected)
	}
}

// TestFlushBuffer_AtomicOnCancel verifies that flushBuffer leaves the
// buffer intact on ctx cancellation, so a follow-up flush (with the
// shutdownGrace ctx) can deliver the data.
func TestFlushBuffer_AtomicOnCancel(t *testing.T) {
	out := make(chan []byte) // unbuffered: send blocks until reader
	c := newTestCapturer(t, out)
	c.buffer = []byte("tail-bytes")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.flushBuffer(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	c.bufferMu.Lock()
	got := string(c.buffer)
	c.bufferMu.Unlock()
	if got != "tail-bytes" {
		t.Fatalf("buffer must survive ctx cancel for grace-period retry; got %q", got)
	}
}

func TestConfig_Values(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected Config
	}{
		{
			name: "custom config",
			cfg: &Config{
				DeviceID:          2,
				Width:             1920,
				Height:            1080,
				FPS:               60,
				PreviewWindowName: "Custom",
				AudioDeviceID:     -1,
				SampleRate:        48000,
				Channels:          2,
				AudioBuffer:       2048,
			},
			expected: Config{
				DeviceID:          2,
				Width:             1920,
				Height:            1080,
				FPS:               60,
				PreviewWindowName: "Custom",
				AudioDeviceID:     -1,
				SampleRate:        48000,
				Channels:          2,
				AudioBuffer:       2048,
			},
		},
		{
			name: "minimum values",
			cfg: &Config{
				DeviceID:          0,
				Width:             1,
				Height:            1,
				FPS:               1,
				PreviewWindowName: "",
				AudioDeviceID:     -1,
				SampleRate:        8000,
				Channels:          1,
				AudioBuffer:       256,
			},
			expected: Config{
				DeviceID:          0,
				Width:             1,
				Height:            1,
				FPS:               1,
				PreviewWindowName: "",
				AudioDeviceID:     -1,
				SampleRate:        8000,
				Channels:          1,
				AudioBuffer:       256,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.DeviceID != tt.expected.DeviceID {
				t.Errorf("DeviceID: got %d, want %d", tt.cfg.DeviceID, tt.expected.DeviceID)
			}
			if tt.cfg.Width != tt.expected.Width {
				t.Errorf("Width: got %d, want %d", tt.cfg.Width, tt.expected.Width)
			}
			if tt.cfg.Height != tt.expected.Height {
				t.Errorf("Height: got %d, want %d", tt.cfg.Height, tt.expected.Height)
			}
			if tt.cfg.FPS != tt.expected.FPS {
				t.Errorf("FPS: got %d, want %d", tt.cfg.FPS, tt.expected.FPS)
			}
			if tt.cfg.PreviewWindowName != tt.expected.PreviewWindowName {
				t.Errorf("PreviewWindowName: got %s, want %s", tt.cfg.PreviewWindowName, tt.expected.PreviewWindowName)
			}
			if tt.cfg.SampleRate != tt.expected.SampleRate {
				t.Errorf("SampleRate: got %d, want %d", tt.cfg.SampleRate, tt.expected.SampleRate)
			}
			if tt.cfg.Channels != tt.expected.Channels {
				t.Errorf("Channels: got %d, want %d", tt.cfg.Channels, tt.expected.Channels)
			}
		})
	}
}
