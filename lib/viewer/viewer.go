package viewer

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	client "github.com/celestiaorg/celestia-openrpc"
	"github.com/celestiaorg/celestia-openrpc/types/share"
	"github.com/gen2brain/malgo"
	"gocv.io/x/gocv"

	"github.com/vgonkivs/love/lib/codec"
)

// Viewer subscribes to Celestia blobs and plays video/audio in real-time
type Viewer struct {
	cfg       *Config
	decoder   codec.Decoder
	client    *client.Client
	namespace share.Namespace
	height    uint64

	// Audio player state
	audioCtx      *malgo.AllocatedContext
	audioDevice   *malgo.Device
	audioBuffer   []byte
	audioBufMu    sync.Mutex
	audioRunning  bool
	audioMu       sync.Mutex
	audioInitErr  error
}

// NewViewer creates a new viewer
func NewViewer(cfg *Config, decoder codec.Decoder, namespaceHex string, startHeight uint64) (*Viewer, error) {
	// Parse namespace from hex
	nsBytes, err := hex.DecodeString(namespaceHex)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace hex: %w", err)
	}

	namespace, err := share.NamespaceFromBytes(nsBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}

	return &Viewer{
		cfg:       cfg,
		decoder:   decoder,
		namespace: namespace,
		height:    startHeight,
	}, nil
}

// Connect establishes connection to Celestia node
func (v *Viewer) Connect(ctx context.Context) error {
	c, err := client.NewClient(ctx, v.cfg.NodeURL, v.cfg.AuthToken)
	if err != nil {
		return fmt.Errorf("failed to connect to Celestia node: %w", err)
	}
	v.client = c
	log.Printf("Viewer: connected to Celestia node at %s", v.cfg.NodeURL)
	return nil
}

// Close closes the connection to Celestia node
func (v *Viewer) Close() error {
	if v.client != nil {
		v.client.Close()
	}
	return nil
}

// startAudioPlayer initializes and starts audio playback
func (v *Viewer) startAudioPlayer(sampleRate, channels int) error {
	malgoCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	v.audioCtx = malgoCtx

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.PeriodSizeInFrames = 1024
	deviceConfig.Alsa.NoMMap = 1

	onSendFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		v.audioBufMu.Lock()
		defer v.audioBufMu.Unlock()

		bytesNeeded := int(frameCount) * channels * 2

		if len(v.audioBuffer) >= bytesNeeded {
			copy(outputSamples, v.audioBuffer[:bytesNeeded])
			v.audioBuffer = v.audioBuffer[bytesNeeded:]
		} else {
			copy(outputSamples, v.audioBuffer)
			for i := len(v.audioBuffer); i < bytesNeeded; i++ {
				outputSamples[i] = 0
			}
			v.audioBuffer = v.audioBuffer[:0]
		}
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onSendFrames,
	}

	device, err := malgo.InitDevice(v.audioCtx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		v.audioCtx.Uninit()
		v.audioCtx.Free()
		return err
	}
	v.audioDevice = device

	if err := device.Start(); err != nil {
		device.Uninit()
		v.audioCtx.Uninit()
		v.audioCtx.Free()
		return err
	}

	v.audioMu.Lock()
	v.audioRunning = true
	v.audioMu.Unlock()

	log.Printf("Viewer: audio player started (sample rate: %d Hz, channels: %d)", sampleRate, channels)
	return nil
}

// stopAudioPlayer stops and cleans up audio playback
func (v *Viewer) stopAudioPlayer() {
	v.audioMu.Lock()
	defer v.audioMu.Unlock()

	if !v.audioRunning {
		return
	}

	v.audioRunning = false
	if v.audioDevice != nil {
		v.audioDevice.Stop()
		v.audioDevice.Uninit()
	}
	if v.audioCtx != nil {
		v.audioCtx.Uninit()
		v.audioCtx.Free()
	}
	log.Println("Viewer: audio player stopped")
}

// playAudio adds audio data to the playback buffer
func (v *Viewer) playAudio(data []byte) {
	v.audioBufMu.Lock()
	defer v.audioBufMu.Unlock()
	v.audioBuffer = append(v.audioBuffer, data...)
}

// Run starts the viewer, fetching blobs, decoding and displaying frames with audio
func (v *Viewer) Run(ctx context.Context) error {
	if v.client == nil {
		return fmt.Errorf("not connected to Celestia node")
	}

	currentHeight := v.height

	// First, try to read entrypoint blob
	log.Printf("Viewer: looking for entrypoint blob at height %d", currentHeight)
	var sampleRate, channels, fps int
	foundEntrypoint := false

	for !foundEntrypoint {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		blobs, err := v.client.Blob.GetAll(ctx, currentHeight, []share.Namespace{v.namespace})
		if err != nil {
			time.Sleep(v.cfg.PollDelay)
			continue
		}

		for _, b := range blobs {
			sr, ch, f, valid := v.decoder.ParseEntrypoint(b.Data)
			if valid {
				sampleRate = sr
				channels = ch
				fps = f
				foundEntrypoint = true
				log.Printf("Viewer: found entrypoint - sample rate: %d, channels: %d, fps: %d",
					sampleRate, channels, fps)
				break
			}
		}

		if !foundEntrypoint {
			currentHeight++
		}
	}

	// Move to next height after entrypoint
	currentHeight++

	// Create display window
	window := gocv.NewWindow(v.cfg.WindowName)
	defer window.Close()

	// Start audio player (graceful degradation if fails)
	if err := v.startAudioPlayer(sampleRate, channels); err != nil {
		log.Printf("Viewer: audio player failed to start: %v (continuing with video only)", err)
		v.audioInitErr = err
	}
	defer v.stopAudioPlayer()

	frameBuffer := make([]byte, 0)
	videoFrameCount := 0
	audioChunkCount := 0

	// For A/V sync timing
	var playbackStartTime time.Time
	playbackStarted := false

	// Unused variable to match expected FPS
	_ = fps

	log.Printf("Viewer: starting playback from height %d, namespace: %s",
		currentHeight, hex.EncodeToString(v.namespace))

	for {
		select {
		case <-ctx.Done():
			log.Printf("Viewer: stopping, displayed %d video frames, played %d audio chunks",
				videoFrameCount, audioChunkCount)
			return nil
		default:
		}

		// Fetch blobs at current height
		blobs, err := v.client.Blob.GetAll(ctx, currentHeight, []share.Namespace{v.namespace})
		if err != nil {
			time.Sleep(v.cfg.PollDelay)
			continue
		}

		// Process blobs directly (no sequence handling needed for sync mode)
		for _, b := range blobs {
			// Skip entrypoint blobs
			if _, _, _, valid := v.decoder.ParseEntrypoint(b.Data); valid {
				continue
			}
			frameBuffer = append(frameBuffer, b.Data...)
		}

		// Decode and display frames from buffer
		for {
			frame, consumed := v.decoder.Decode(frameBuffer)
			if frame == nil {
				break
			}

			// Initialize playback timing on first frame
			if !playbackStarted {
				playbackStartTime = time.Now()
				playbackStarted = true
			}

			// Calculate expected playback time based on frame timestamp
			expectedTime := playbackStartTime.Add(time.Duration(frame.Timestamp))
			waitTime := time.Until(expectedTime)

			// Wait if we're ahead of schedule (but don't wait too long)
			if waitTime > 0 && waitTime < 500*time.Millisecond {
				time.Sleep(waitTime)
			}

			switch frame.Type {
			case codec.FrameTypeVideo:
				if frame.VideoFrame != nil {
					window.IMShow(*frame.VideoFrame)
					key := window.WaitKey(1)
					if key == 27 {
						log.Println("Viewer: user closed window")
						frame.VideoFrame.Close()
						return nil
					}
					frame.VideoFrame.Close()
					videoFrameCount++
				}

			case codec.FrameTypeAudio:
				if v.audioInitErr == nil {
					v.playAudio(frame.AudioData)
					audioChunkCount++
				}
			}

			frameBuffer = frameBuffer[consumed:]
		}

		currentHeight++
	}
}
