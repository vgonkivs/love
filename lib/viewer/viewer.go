package viewer

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-node/api/client"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/gen2brain/malgo"
	"gocv.io/x/gocv"

	"github.com/vgonkivs/love/lib/codec"
)

// Viewer subscribes to Celestia blobs and plays video/audio in real-time
type Viewer struct {
	cfg       *Config
	decoder   codec.Decoder
	h264Dec   *codec.H264Decoder // For H.264 streams (needs Start/Close)
	client    *client.ReadClient
	namespace share.Namespace
	height    uint64
	live      bool // If true, subscribe to live blobs after reading entrypoint

	// Audio player state
	audioCtx     *malgo.AllocatedContext
	audioDevice  *malgo.Device
	audioBuffer  []byte
	audioBufMu   sync.Mutex
	audioRunning bool
	audioMu      sync.Mutex
	audioInitErr error
}

// NewViewer creates a new viewer
// Decoder is created automatically based on entrypoint blob
// If live is true, viewer will subscribe to live blobs after reading entrypoint
func NewViewer(cfg *Config, namespaceHex string, startHeight uint64, live bool) (*Viewer, error) {
	// Parse namespace from hex
	nsBytes, err := hex.DecodeString(namespaceHex)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace hex: %w", err)
	}

	namespace, err := share.NewNamespaceFromBytes(nsBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}

	return &Viewer{
		cfg:       cfg,
		namespace: namespace,
		height:    startHeight,
		live:      live,
	}, nil
}

// Connect establishes connection to Celestia node
func (v *Viewer) Connect(ctx context.Context) error {
	cli, err := client.NewReadClient(ctx, client.ReadConfig{
		BridgeDAAddr: v.cfg.NodeURL,
		DAAuthToken:  v.cfg.AuthToken,
		EnableDATLS:  false,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to Celestia node: %w", err)
	}
	v.client = cli
	log.Printf("Viewer: connected to Celestia node at %s", v.cfg.NodeURL)
	return nil
}

// Close closes the connection to Celestia node
func (v *Viewer) Close() error {
	if v.client != nil {
		return v.client.Close()
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

	// Get entrypoint blob at specified height
	log.Printf("Viewer: looking for entrypoint blob at height %d", v.height)

	blobs, err := v.client.Blob.GetAll(ctx, v.height, []share.Namespace{v.namespace})
	if err != nil {
		return fmt.Errorf("failed to get blobs at height %d: %w", v.height, err)
	}
	if len(blobs) == 0 {
		return fmt.Errorf("no blobs found at height %d", v.height)
	}

	sampleRate, channels, width, height, isH264, err := codec.ParseH264Entrypoint(blobs[0].Data())
	if err != nil {
		return fmt.Errorf("invalid entrypoint blob at height %d", v.height)
	}

	log.Printf("Viewer: found entrypoint at height %d - sample rate: %d, channels: %d, codec: %s",
		v.height, sampleRate, channels, map[bool]string{true: "H.264", false: "JPEG"}[isH264])
	if isH264 {
		log.Printf("Viewer: video dimensions: %dx%d", width, height)
	}

	// Create decoder based on detected codec type
	if isH264 {
		decoderCfg := codec.H264DecoderConfig{
			Width:  width,
			Height: height,
		}
		v.h264Dec = codec.NewH264Decoder(decoderCfg)
		v.decoder = v.h264Dec
		if err := v.h264Dec.Start(); err != nil {
			return fmt.Errorf("failed to start H.264 decoder: %w", err)
		}
		defer v.h264Dec.Close()
		log.Printf("Viewer: H.264 decoder started")
	} else {
		v.decoder = codec.NewJPEGCodec(85)
		log.Printf("Viewer: JPEG decoder initialized")
	}

	// Create display window
	window := gocv.NewWindow(v.cfg.WindowName)
	defer window.Close()

	// Start audio player (graceful degradation if fails)
	if err := v.startAudioPlayer(sampleRate, channels); err != nil {
		log.Printf("Viewer: audio player failed to start: %v (continuing with video only)", err)
		v.audioInitErr = err
	}
	defer v.stopAudioPlayer()

	videoFrameCount := 0
	audioChunkCount := 0

	log.Printf("Viewer: starting playback from height %d, namespace: %s, mode: %s",
		v.height, hex.EncodeToString(v.namespace.Bytes()), map[bool]string{true: "live", false: "historical"}[v.live])

	// Start background blob fetcher
	blobChan := make(chan []byte, 10) // Buffer up to 10 blobs
	fetchCtx, fetchCancel := context.WithCancel(ctx)
	defer fetchCancel()

	if v.live {
		go v.subscribeBlobs(fetchCtx, blobChan)
	} else {
		go v.fetchBlobs(fetchCtx, v.height, blobChan)
	}

	// Playback loop
	frameBuffer := make([]byte, 0)
	var firstVideoTimestamp uint64
	var playbackStartTime time.Time
	videoTimingStarted := false

	for {
		// Check for cancellation
		select {
		case <-ctx.Done():
			log.Printf("Viewer: stopping, displayed %d video frames, played %d audio chunks",
				videoFrameCount, audioChunkCount)
			return nil
		default:
		}

		// Non-blocking refill from blob channel
	refillLoop:
		for {
			select {
			case data, ok := <-blobChan:
				if !ok {
					if len(frameBuffer) == 0 {
						log.Printf("Viewer: blob channel closed, displayed %d video frames", videoFrameCount)
						return nil
					}
					break refillLoop
				}
				frameBuffer = append(frameBuffer, data...)
			default:
				break refillLoop
			}
		}

		// Decode and display frames from buffer
		frame, consumed := v.decoder.Decode(frameBuffer)
		if consumed == 0 {
			// Not enough data, wait for more (blocking)
			log.Printf("Viewer: waiting for data (buffer: %d bytes, frames: %d)", len(frameBuffer), videoFrameCount)
			select {
			case <-ctx.Done():
				return nil
			case data, ok := <-blobChan:
				if !ok {
					return nil
				}
				frameBuffer = append(frameBuffer, data...)
			}
			continue
		}

		if frame == nil {
			// Unknown marker, skip
			frameBuffer = frameBuffer[consumed:]
			continue
		}

		// Skip FrameTypeNone (H.264 frame still being processed)
		if frame.Type == codec.FrameTypeNone {
			frameBuffer = frameBuffer[consumed:]
			// Check for decoded frames from H.264 decoder
			if v.h264Dec != nil {
				for _, videoFrame := range v.h264Dec.DrainFrames() {
					if videoFrame != nil {
						window.IMShow(*videoFrame)
						if window.WaitKey(1) == 27 {
							videoFrame.Close()
							return nil
						}
						videoFrame.Close()
						videoFrameCount++
					}
				}
			}
			continue
		}

		switch frame.Type {
		case codec.FrameTypeVideo:
			if frame.VideoFrame != nil {
				// Pace video based on timestamps
				if !videoTimingStarted {
					firstVideoTimestamp = frame.Timestamp
					playbackStartTime = time.Now()
					videoTimingStarted = true
				} else {
					// Calculate when this frame should be displayed
					elapsed := frame.Timestamp - firstVideoTimestamp
					targetTime := playbackStartTime.Add(time.Duration(elapsed))
					waitTime := time.Until(targetTime)
					if waitTime > 0 && waitTime < time.Second {
						time.Sleep(waitTime)
					}
				}

				window.IMShow(*frame.VideoFrame)
				if window.WaitKey(1) == 27 {
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
}

// fetchBlobs fetches blobs in the background and sends data to channel (historical mode)
func (v *Viewer) fetchBlobs(ctx context.Context, startHeight uint64, out chan<- []byte) {
	defer close(out)
	currentHeight := startHeight
	blobsSent := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		blobs, err := v.client.Blob.GetAll(ctx, currentHeight, []share.Namespace{v.namespace})
		if err != nil || len(blobs) == 0 {
			currentHeight++
			continue
		}

		for _, b := range blobs {
			select {
			case <-ctx.Done():
				return
			case out <- b.Data():
				blobsSent++
				log.Printf("Fetcher: sent blob %d (%d bytes) from height %d", blobsSent, len(b.Data()), currentHeight)
			}
		}

		currentHeight++
	}
}

// subscribeBlobs subscribes to blobs in live mode and sends data to channel
func (v *Viewer) subscribeBlobs(ctx context.Context, out chan<- []byte) {
	defer close(out)

	sub, err := v.client.Blob.Subscribe(ctx, v.namespace)
	if err != nil {
		log.Printf("Subscriber: failed to subscribe to blobs: %v", err)
		return
	}

	log.Printf("Subscriber: subscribed to namespace %s", hex.EncodeToString(v.namespace.Bytes()))
	blobsSent := 0

	for {
		select {
		case <-ctx.Done():
			log.Printf("Subscriber: stopping, sent %d blobs", blobsSent)
			return
		case resp, ok := <-sub:
			if !ok {
				log.Printf("Subscriber: subscription channel closed, sent %d blobs", blobsSent)
				return
			}

			// Process all blobs in the response
			for _, b := range resp.Blobs {
				select {
				case <-ctx.Done():
					return
				case out <- b.Data():
					blobsSent++
					log.Printf("Subscriber: sent blob %d (%d bytes) from height %d", blobsSent, len(b.Data()), resp.Height)
				}
			}
		}
	}
}
