package capture

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
	"gocv.io/x/gocv"

	"github.com/vgonkivs/love/lib/codec"
)

// Capturer captures video and audio, encodes them, and outputs ready blobs
type Capturer struct {
	cfg     *Config
	encoder codec.Encoder

	// Internal state
	cam          *gocv.VideoCapture
	audioCtx     *malgo.AllocatedContext
	audioDevice  *malgo.Device
	audioRunning bool
	audioMu      sync.Mutex
	startTime    time.Time
	sequence     uint32
	sequenceMu   sync.Mutex
	buffer       []byte
	bufferMu     sync.Mutex
	output       chan<- []byte
	audioInitErr error
}

// NewCapturer creates a new video/audio capturer
func NewCapturer(cfg *Config, encoder codec.Encoder) *Capturer {
	return &Capturer{
		cfg:     cfg,
		encoder: encoder,
		buffer:  make([]byte, 0, codec.ChunkSize),
	}
}

// nextSequence returns the next sequence number (thread-safe)
func (c *Capturer) nextSequence() uint32 {
	c.sequenceMu.Lock()
	defer c.sequenceMu.Unlock()
	seq := c.sequence
	c.sequence++
	return seq
}

// addToBuffer adds encoded data to buffer and emits chunks when ready
func (c *Capturer) addToBuffer(ctx context.Context, data []byte) {
	c.bufferMu.Lock()
	defer c.bufferMu.Unlock()

	c.buffer = append(c.buffer, data...)

	// Emit full chunks
	for len(c.buffer) >= codec.ChunkSize {
		chunk := make([]byte, codec.ChunkSize)
		copy(chunk, c.buffer[:codec.ChunkSize])

		select {
		case c.output <- chunk:
		case <-ctx.Done():
			return
		default:
			// Channel full, drop chunk to prevent preview freeze
			log.Println("Capturer: output channel full, dropping chunk")
		}

		c.buffer = c.buffer[codec.ChunkSize:]
	}
}

// flushBuffer sends any remaining data in the buffer
func (c *Capturer) flushBuffer(ctx context.Context) {
	c.bufferMu.Lock()
	defer c.bufferMu.Unlock()

	if len(c.buffer) > 0 {
		select {
		case c.output <- c.buffer:
		case <-ctx.Done():
		default:
		}
		c.buffer = nil
	}
}

// startAudioCapture initializes and starts audio capture
func (c *Capturer) startAudioCapture(ctx context.Context) error {
	malgoCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	c.audioCtx = malgoCtx

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = uint32(c.cfg.Channels)
	deviceConfig.SampleRate = uint32(c.cfg.SampleRate)
	deviceConfig.PeriodSizeInFrames = uint32(c.cfg.AudioBuffer)
	deviceConfig.Alsa.NoMMap = 1

	// Buffer for accumulating samples
	var sampleBuffer []byte
	var bufMu sync.Mutex

	// Samples to accumulate before sending (roughly 100ms worth)
	samplesPerSend := c.cfg.SampleRate / 10 * c.cfg.Channels * 2

	onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		bufMu.Lock()
		defer bufMu.Unlock()

		sampleBuffer = append(sampleBuffer, inputSamples...)

		for len(sampleBuffer) >= samplesPerSend {
			chunk := make([]byte, samplesPerSend)
			copy(chunk, sampleBuffer[:samplesPerSend])
			sampleBuffer = sampleBuffer[samplesPerSend:]

			// Encode audio and add to buffer
			timestamp := time.Since(c.startTime)
			seq := c.nextSequence()

			encoded, err := c.encoder.EncodeAudio(chunk, timestamp, seq)
			if err != nil {
				log.Printf("Capturer: failed to encode audio: %v", err)
				continue
			}

			go c.addToBuffer(ctx, encoded)
		}
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}

	device, err := malgo.InitDevice(c.audioCtx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		c.audioCtx.Uninit()
		c.audioCtx.Free()
		return err
	}
	c.audioDevice = device

	if err := device.Start(); err != nil {
		device.Uninit()
		c.audioCtx.Uninit()
		c.audioCtx.Free()
		return err
	}

	c.audioMu.Lock()
	c.audioRunning = true
	c.audioMu.Unlock()

	log.Printf("Capturer: audio started (sample rate: %d Hz, channels: %d)", c.cfg.SampleRate, c.cfg.Channels)
	return nil
}

// stopAudioCapture stops and cleans up audio capture
func (c *Capturer) stopAudioCapture() {
	c.audioMu.Lock()
	defer c.audioMu.Unlock()

	if !c.audioRunning {
		return
	}

	c.audioRunning = false
	if c.audioDevice != nil {
		c.audioDevice.Stop()
		c.audioDevice.Uninit()
	}
	if c.audioCtx != nil {
		c.audioCtx.Uninit()
		c.audioCtx.Free()
	}
	log.Println("Capturer: audio stopped")
}

// StartableEncoder is an optional interface for encoders that need initialization
type StartableEncoder interface {
	Start() error
	Close() error
}

// Run starts capturing video and audio, encoding frames, and outputting blobs.
// This method blocks until context is cancelled or an error occurs.
// IMPORTANT: Must be called from the main goroutine on macOS for OpenCV GUI.
func (c *Capturer) Run(ctx context.Context, output chan<- []byte) error {
	c.output = output
	c.startTime = time.Now()

	// Start encoder if it requires initialization (e.g., H.264)
	if startable, ok := c.encoder.(StartableEncoder); ok {
		if err := startable.Start(); err != nil {
			return err
		}
		defer startable.Close()
		log.Printf("Capturer: encoder started")
	}

	// Send entrypoint blob first (before camera initialization)
	entrypoint := c.GetEntrypoint()
	select {
	case output <- entrypoint:
		log.Printf("Capturer: sent entrypoint blob")
	case <-ctx.Done():
		return nil
	}

	// Open camera
	cam, err := gocv.OpenVideoCapture(c.cfg.DeviceID)
	if err != nil {
		return err
	}
	c.cam = cam
	defer cam.Close()

	// Configure camera
	cam.Set(gocv.VideoCaptureFrameWidth, float64(c.cfg.Width))
	cam.Set(gocv.VideoCaptureFrameHeight, float64(c.cfg.Height))
	cam.Set(gocv.VideoCaptureFPS, float64(c.cfg.FPS))
	cam.Set(gocv.VideoCaptureBufferSize, 1)

	actualWidth := cam.Get(gocv.VideoCaptureFrameWidth)
	actualHeight := cam.Get(gocv.VideoCaptureFrameHeight)
	actualFPS := cam.Get(gocv.VideoCaptureFPS)

	log.Printf("Capturer: camera configured %.0fx%.0f@%.1ffps (requested: %dx%d@%dfps)",
		actualWidth, actualHeight, actualFPS,
		c.cfg.Width, c.cfg.Height, c.cfg.FPS)

	// Start audio capture (graceful degradation if fails)
	if err := c.startAudioCapture(ctx); err != nil {
		log.Printf("Capturer: audio capture failed to start: %v (continuing with video only)", err)
		c.audioInitErr = err
	}
	defer c.stopAudioCapture()

	// Setup preview window if enabled
	var previewWindow *gocv.Window
	if c.cfg.EnablePreview {
		previewWindow = gocv.NewWindow(c.cfg.PreviewWindowName)
		defer previewWindow.Close()
		log.Printf("Capturer: local preview enabled")
	}

	frame := gocv.NewMat()
	defer frame.Close()

	frameDuration := time.Second / time.Duration(c.cfg.FPS)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	log.Printf("Capturer: starting capture loop at %d FPS", c.cfg.FPS)

	for {
		select {
		case <-ctx.Done():
			c.flushBuffer(ctx)
			c.sendStreamEnd(ctx)
			log.Println("Capturer: stopping")
			return nil

		case <-ticker.C:
			if ok := cam.Read(&frame); !ok {
				log.Println("Capturer: failed to read frame")
				continue
			}

			if frame.Empty() {
				continue
			}

			// Display frame in preview window if enabled
			if previewWindow != nil {
				previewWindow.IMShow(frame)
				if previewWindow.WaitKey(1) == 27 { // ESC key
					log.Println("Capturer: preview window closed by user")
					c.flushBuffer(ctx)
					c.sendStreamEnd(ctx)
					return nil
				}
			}

			// Encode video frame
			timestamp := time.Since(c.startTime)
			seq := c.nextSequence()

			encoded, err := c.encoder.EncodeVideo(frame, timestamp, seq)
			if err != nil {
				log.Printf("Capturer: failed to encode video frame: %v", err)
				continue
			}

			// H.264 encoder may return nil when frame is still being processed
			if encoded != nil {
				go c.addToBuffer(ctx, encoded)
			}
		}
	}
}

// GetEntrypoint returns the entrypoint blob for this stream
func (c *Capturer) GetEntrypoint() []byte {
	return c.encoder.CreateEntrypoint(c.cfg.SampleRate, c.cfg.Channels)
}

// sendStreamEnd sends the stream end notification blob
func (c *Capturer) sendStreamEnd(ctx context.Context) {
	totalDuration := time.Since(c.startTime)
	totalFrames := c.sequence // sequence is the total frame count
	streamEnd := c.encoder.CreateStreamEnd(totalDuration, totalFrames)

	select {
	case c.output <- streamEnd:
		log.Printf("Capturer: sent stream end blob (duration: %v, frames: %d)", totalDuration, totalFrames)
	case <-ctx.Done():
	default:
	}
}

// AudioFailed returns true if audio initialization failed
func (c *Capturer) AudioFailed() bool {
	return c.audioInitErr != nil
}
