package audio

import (
	"context"
	"log"
	"sync"

	"github.com/gen2brain/malgo"
)

// Player plays audio samples received from a channel
type Player struct {
	cfg *Config

	ctx       *malgo.AllocatedContext
	device    *malgo.Device
	mu        sync.Mutex
	isRunning bool

	// Audio buffer for playback
	bufMu    sync.Mutex
	buffer   []byte
	bufferCh chan []byte
}

// NewPlayer creates a new audio player
func NewPlayer(cfg *Config) *Player {
	return &Player{
		cfg:      cfg,
		bufferCh: make(chan []byte, 100),
	}
}

// Run starts playing audio from the input channel.
// This method blocks until context is cancelled.
func (p *Player) Run(ctx context.Context, input <-chan []byte) error {
	// Initialize malgo context
	malgoCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	p.ctx = malgoCtx
	defer func() {
		_ = malgoCtx.Uninit()
		malgoCtx.Free()
	}()

	// Configure device for playback
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = uint32(p.cfg.Channels)
	deviceConfig.SampleRate = uint32(p.cfg.SampleRate)
	deviceConfig.PeriodSizeInFrames = uint32(p.cfg.BufferSize)
	deviceConfig.Alsa.NoMMap = 1

	// Callback for providing audio data
	onSendFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		p.bufMu.Lock()
		defer p.bufMu.Unlock()

		bytesNeeded := int(frameCount) * p.cfg.Channels * 2 // 16-bit = 2 bytes per sample

		if len(p.buffer) >= bytesNeeded {
			copy(outputSamples, p.buffer[:bytesNeeded])
			p.buffer = p.buffer[bytesNeeded:]
		} else {
			// Not enough data, fill with silence
			copy(outputSamples, p.buffer)
			// Fill remaining with zeros (silence)
			for i := len(p.buffer); i < bytesNeeded; i++ {
				outputSamples[i] = 0
			}
			p.buffer = p.buffer[:0]
		}
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onSendFrames,
	}

	device, err := malgo.InitDevice(p.ctx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		return err
	}
	p.device = device
	defer device.Uninit()

	// Start the device
	if err := device.Start(); err != nil {
		return err
	}
	p.mu.Lock()
	p.isRunning = true
	p.mu.Unlock()

	log.Printf("AudioPlayer: started (sample rate: %d Hz, channels: %d)", p.cfg.SampleRate, p.cfg.Channels)

	// Receive audio data from input channel
	for {
		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.isRunning = false
			p.mu.Unlock()
			device.Stop()
			log.Println("AudioPlayer: stopped")
			return nil

		case audioData, ok := <-input:
			if !ok {
				p.mu.Lock()
				p.isRunning = false
				p.mu.Unlock()
				device.Stop()
				log.Println("AudioPlayer: input closed, stopped")
				return nil
			}

			// Add audio data to buffer
			p.bufMu.Lock()
			p.buffer = append(p.buffer, audioData...)
			p.bufMu.Unlock()
		}
	}
}
