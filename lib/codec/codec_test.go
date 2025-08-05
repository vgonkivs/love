package codec

import (
	"context"
	"sync"
	"testing"
	"time"

	"gocv.io/x/gocv"
)

func TestNewCodec(t *testing.T) {
	cfg := &Config{JPEGQuality: 90}
	codec := NewCodec(cfg)

	if codec == nil {
		t.Fatal("expected non-nil codec")
	}
	if codec.cfg.JPEGQuality != 90 {
		t.Errorf("expected JPEGQuality 90, got %d", codec.cfg.JPEGQuality)
	}
}

func TestChunkSize(t *testing.T) {
	if ChunkSize != 1048576 {
		t.Errorf("expected ChunkSize 1048576 (1MB), got %d", ChunkSize)
	}
}

func TestCodec_Run_SingleFrame(t *testing.T) {
	cfg := &Config{JPEGQuality: 85}
	codec := NewCodec(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := make(chan gocv.Mat, 1)
	output := make(chan []byte, 10)

	// Create a test frame
	frame := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	defer frame.Close()

	// Start codec in goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		codec.Run(ctx, input, output)
	}()

	// Send frame
	input <- frame.Clone()
	close(input)

	// Wait for codec to finish
	wg.Wait()
	close(output)

	// A single small frame shouldn't produce a full 1MB chunk
	// It should be flushed at the end
	var blobs [][]byte
	for blob := range output {
		blobs = append(blobs, blob)
	}

	if len(blobs) != 1 {
		t.Errorf("expected 1 blob (flushed), got %d", len(blobs))
	}

	// The blob should be smaller than ChunkSize
	if len(blobs) > 0 && len(blobs[0]) >= ChunkSize {
		t.Errorf("expected blob smaller than %d, got %d", ChunkSize, len(blobs[0]))
	}
}

func TestCodec_Run_ProducesChunks(t *testing.T) {
	cfg := &Config{JPEGQuality: 95} // Higher quality = larger files
	codec := NewCodec(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := make(chan gocv.Mat, 100)
	output := make(chan []byte, 10)

	// Start codec in goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		codec.Run(ctx, input, output)
	}()

	// Send many large frames to trigger chunking
	// A 500x500 image at quality 95 is roughly 50-100KB
	// We need about 10-20 frames to fill 1MB
	for i := 0; i < 30; i++ {
		frame := gocv.NewMatWithSize(500, 500, gocv.MatTypeCV8UC3)
		// Add some variation to the image
		for y := 0; y < 500; y++ {
			for x := 0; x < 500; x++ {
				frame.SetUCharAt(y, x*3, uint8((x+i*10)%256))
				frame.SetUCharAt(y, x*3+1, uint8((y+i*10)%256))
				frame.SetUCharAt(y, x*3+2, uint8((x+y+i*10)%256))
			}
		}
		input <- frame
	}
	close(input)

	// Collect output
	var blobs [][]byte
	done := make(chan struct{})
	go func() {
		for blob := range output {
			blobs = append(blobs, blob)
		}
		close(done)
	}()

	wg.Wait()
	close(output)
	<-done

	// Should have at least one full chunk
	if len(blobs) < 1 {
		t.Error("expected at least 1 blob")
	}

	// Check that full chunks are exactly ChunkSize
	for i, blob := range blobs {
		if i < len(blobs)-1 {
			// All but the last should be exactly ChunkSize
			if len(blob) != ChunkSize {
				t.Errorf("blob %d: expected size %d, got %d", i, ChunkSize, len(blob))
			}
		}
	}
}

func TestCodec_Run_ContextCancellation(t *testing.T) {
	cfg := &Config{JPEGQuality: 85}
	codec := NewCodec(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan gocv.Mat, 10)
	output := make(chan []byte, 10)

	// Start codec
	done := make(chan error)
	go func() {
		done <- codec.Run(ctx, input, output)
	}()

	// Send a frame
	frame := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	input <- frame

	// Cancel context
	cancel()

	// Should return without error
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("codec did not stop after context cancellation")
	}
}

func TestCodec_Run_EmptyFrame(t *testing.T) {
	cfg := &Config{JPEGQuality: 85}
	codec := NewCodec(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := make(chan gocv.Mat, 2)
	output := make(chan []byte, 10)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		codec.Run(ctx, input, output)
	}()

	// Send a valid frame followed by closing
	frame := gocv.NewMatWithSize(50, 50, gocv.MatTypeCV8UC3)
	input <- frame.Clone()
	frame.Close()
	close(input)

	wg.Wait()
	close(output)

	// Should have processed the frame
	count := 0
	for range output {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 output blob, got %d", count)
	}
}

func TestCodec_JPEGQuality(t *testing.T) {
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

	lowQBuf, err := gocv.IMEncodeWithParams(".jpg", frame, []int{gocv.IMWriteJpegQuality, 20})
	if err != nil {
		t.Fatalf("failed to encode low quality: %v", err)
	}
	defer lowQBuf.Close()

	highQBuf, err := gocv.IMEncodeWithParams(".jpg", frame, []int{gocv.IMWriteJpegQuality, 95})
	if err != nil {
		t.Fatalf("failed to encode high quality: %v", err)
	}
	defer highQBuf.Close()

	lowSize := len(lowQBuf.GetBytes())
	highSize := len(highQBuf.GetBytes())

	if highSize <= lowSize {
		t.Errorf("expected high quality (%d) > low quality (%d)", highSize, lowSize)
	}
}
