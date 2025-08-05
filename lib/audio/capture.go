package audio

import (
	"context"
	"log"
	"sync"

	"github.com/gen2brain/malgo"
)

// AudioData represents a chunk of captured audio
type AudioData struct {
	Samples    []byte // PCM audio samples (16-bit signed, little-endian)
	SampleRate int
	Channels   int
}

// Capturer captures audio from microphone and sends it to a channel
type Capturer struct {
	cfg *Config

	ctx       *malgo.AllocatedContext
	device    *malgo.Device
	mu        sync.Mutex
	isRunning bool
}

// NewCapturer creates a new audio capturer
func NewCapturer(cfg *Config) *Capturer {
	return &Capturer{
		cfg: cfg,
	}
}

// Run starts capturing audio and sends samples to the output channel.
// This method blocks until context is cancelled.
func (c *Capturer) Run(ctx context.Context, output chan<- AudioData) error {
	// Initialize malgo context
	malgoCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	c.ctx = malgoCtx
	defer func() {
		_ = malgoCtx.Uninit()
		malgoCtx.Free()
	}()

	// Configure device
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = uint32(c.cfg.Channels)
	deviceConfig.SampleRate = uint32(c.cfg.SampleRate)
	deviceConfig.PeriodSizeInFrames = uint32(c.cfg.BufferSize)
	deviceConfig.Alsa.NoMMap = 1

	// Buffer for accumulating samples
	var sampleBuffer []byte
	var bufMu sync.Mutex

	// Samples to accumulate before sending (roughly 100ms worth)
	samplesPerSend := c.cfg.SampleRate / 10 * c.cfg.Channels * 2 // 16-bit = 2 bytes per sample

	// Callback for receiving audio data
	onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		bufMu.Lock()
		defer bufMu.Unlock()

		// Append incoming samples to buffer
		sampleBuffer = append(sampleBuffer, inputSamples...)

		// When we have enough samples, send them
		for len(sampleBuffer) >= samplesPerSend {
			chunk := make([]byte, samplesPerSend)
			copy(chunk, sampleBuffer[:samplesPerSend])
			sampleBuffer = sampleBuffer[samplesPerSend:]

			select {
			case output <- AudioData{
				Samples:    chunk,
				SampleRate: c.cfg.SampleRate,
				Channels:   c.cfg.Channels,
			}:
			default:
				// Drop if channel is full
			}
		}
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}

	device, err := malgo.InitDevice(c.ctx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		return err
	}
	c.device = device
	defer device.Uninit()

	// Start the device
	if err := device.Start(); err != nil {
		return err
	}
	c.mu.Lock()
	c.isRunning = true
	c.mu.Unlock()

	log.Printf("AudioCapturer: started (sample rate: %d Hz, channels: %d)", c.cfg.SampleRate, c.cfg.Channels)

	// Wait for context cancellation
	<-ctx.Done()

	c.mu.Lock()
	c.isRunning = false
	c.mu.Unlock()

	device.Stop()
	log.Println("AudioCapturer: stopped")

	return nil
}
