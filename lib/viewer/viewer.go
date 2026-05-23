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

// liveEntrypointSearchWindow caps how many blocks past the user-supplied
// -height the live viewer will scan for the entrypoint. ~10 minutes of
// Celestia headers — enough slack for users picking the wrong start
// block, without indefinitely walking the chain if the publisher never
// posted in this namespace.
const liveEntrypointSearchWindow = 100

// Viewer subscribes to Celestia blobs and plays video/audio in real-time
type Viewer struct {
	cfg       *Config
	decoder   codec.Decoder
	h264Dec   *codec.H264Decoder // For H.264 streams (needs Start/Close)
	client    *client.ReadClient
	namespace share.Namespace
	height    uint64

	// Audio player state
	audioCtx      *malgo.AllocatedContext
	audioDevice   *malgo.Device
	audioBuffer   []byte
	audioBufMu    sync.Mutex
	audioMaxBytes int // 0 = unbounded; set by startAudioPlayer to maxAudioBufferSeconds worth
	audioRunning  bool
	audioMu       sync.Mutex
	audioInitErr  error
}

// maxAudioBufferSeconds caps how much audio may pile up in the playback
// buffer ahead of video. Without this cap, audio races arbitrarily far
// ahead of the (paced) video stream — a quick patch around the deeper
// "no shared A/V clock" problem (see R2 in the audit). 500ms is small
// enough to be barely noticeable as drift and large enough to absorb
// normal decode jitter.
const maxAudioBufferSeconds = 0.5

// defaultPlaybackFPS is used when the entrypoint advertises fps <= 0.
// Pacing needs a non-zero period to avoid a div-by-zero and to bound
// the display rate when the FrameTypeNone path drains decoded frames.
const defaultPlaybackFPS = 30

// NewViewer creates a new viewer
// Decoder is created automatically based on entrypoint blob
func NewViewer(cfg *Config, namespaceHex string, startHeight uint64) (*Viewer, error) {
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

	// Cap audioBuffer at maxAudioBufferSeconds worth of S16 samples.
	v.audioBufMu.Lock()
	v.audioMaxBytes = int(float64(sampleRate*channels*2) * maxAudioBufferSeconds)
	v.audioBufMu.Unlock()

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

// playAudio adds audio data to the playback buffer, capped at
// audioMaxBytes. When the cap is exceeded — typically because the
// blob fetcher delivered a burst of historical chunks faster than
// the video pacer can advance — we drop the OLDEST samples instead of
// the newest. Keeping the newest preserves the user's perceived
// "now" in audio at the cost of one audible jump, rather than letting
// audio race seconds ahead of video for the rest of the session.
func (v *Viewer) playAudio(data []byte) {
	v.audioBufMu.Lock()
	defer v.audioBufMu.Unlock()
	v.audioBuffer = append(v.audioBuffer, data...)
	if v.audioMaxBytes > 0 && len(v.audioBuffer) > v.audioMaxBytes {
		overflow := len(v.audioBuffer) - v.audioMaxBytes
		v.audioBuffer = v.audioBuffer[overflow:]
	}
}

// Run starts the viewer, fetching blobs, decoding and displaying frames with audio
func (v *Viewer) Run(ctx context.Context) error {
	if v.client == nil {
		return fmt.Errorf("not connected to Celestia node")
	}

	startHeight := v.height
	currentHeight := startHeight

	// First, try to read entrypoint blob to detect codec type.
	// Both modes walk forward from startHeight; live mode is bounded by
	// liveEntrypointSearchWindow so an off-by-a-few startHeight (very
	// common: user picks the wrong block) doesn't make -live exit fatally.
	log.Printf("Viewer: looking for entrypoint blob at height %d", currentHeight)
	var sampleRate, channels, fps, width, height int
	var isH264 bool
	foundEntrypoint := false

	for !foundEntrypoint {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		blobs, err := v.client.Blob.GetAll(ctx, currentHeight, []share.Namespace{v.namespace})
		if err != nil || len(blobs) == 0 {
			if v.cfg.Live && currentHeight-startHeight >= liveEntrypointSearchWindow {
				return fmt.Errorf("no entrypoint blob found in [%d, %d]", startHeight, currentHeight)
			}
			// No blobs at this height, move to next immediately
			currentHeight++
			continue
		}

		for _, b := range blobs {
			// Try to parse as H.264 entrypoint (extended format)
			sr, ch, f, w, h, h264, valid := codec.ParseH264Entrypoint(b.Data())
			if valid {
				sampleRate = sr
				channels = ch
				fps = f
				width = w
				height = h
				isH264 = h264
				foundEntrypoint = true
				log.Printf("Viewer: found entrypoint at height %d - sample rate: %d, channels: %d, fps: %d, codec: %s",
					currentHeight, sampleRate, channels, fps, map[bool]string{true: "H.264", false: "JPEG"}[isH264])
				if isH264 {
					log.Printf("Viewer: video dimensions: %dx%d", width, height)
				}
				break
			}
		}

		if !foundEntrypoint {
			if v.cfg.Live && currentHeight-startHeight >= liveEntrypointSearchWindow {
				return fmt.Errorf("no entrypoint blob found in [%d, %d]", startHeight, currentHeight)
			}
			currentHeight++
		}
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

	videoFrameCount := 0
	audioChunkCount := 0

	// Pacing. We can't trust per-wrapper timestamps for frame-accurate
	// pacing — the H.264 encoder bundles multiple NALs under one wrapper
	// timestamp, so a single wrapper can correspond to a burst of
	// decoded frames (see audit finding #9). Pace strictly by fps from
	// the entrypoint instead. Applies uniformly to both code paths:
	// FrameTypeVideo (decoder returned a frame inline) and FrameTypeNone
	// followed by DrainFrames (decoder buffered frames internally).
	if fps <= 0 {
		log.Printf("Viewer: entrypoint advertised fps=%d, falling back to %d", fps, defaultPlaybackFPS)
		fps = defaultPlaybackFPS
	}
	frameDuration := time.Second / time.Duration(fps)
	var nextFrameAt time.Time // zero = display first frame immediately

	displayFrame := func(m *gocv.Mat) (esc bool) {
		if !nextFrameAt.IsZero() {
			if wait := time.Until(nextFrameAt); wait > 0 {
				time.Sleep(wait)
			}
		}
		now := time.Now()
		// If we fell more than one period behind (e.g. decode stall),
		// resync rather than chasing wall clock with sub-period sleeps.
		if nextFrameAt.IsZero() || nextFrameAt.Before(now.Add(-frameDuration)) {
			nextFrameAt = now.Add(frameDuration)
		} else {
			nextFrameAt = nextFrameAt.Add(frameDuration)
		}
		window.IMShow(*m)
		return window.WaitKey(1) == 27
	}

	log.Printf("Viewer: starting playback from height %d, namespace: %s",
		currentHeight, hex.EncodeToString(v.namespace.Bytes()))

	// Start background blob fetcher
	blobChan := make(chan []byte, 10) // Buffer up to 10 blobs
	fetchCtx, fetchCancel := context.WithCancel(ctx)
	defer fetchCancel()

	if v.cfg.Live {
		go v.fetchBlobsLive(fetchCtx, currentHeight, blobChan)
	} else {
		go v.fetchBlobs(fetchCtx, currentHeight, blobChan)
	}

	// Playback loop
	frameBuffer := make([]byte, 0)

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

		// FrameTypeNone: input bytes consumed but no inline decoded frame.
		// Drain any frames the decoder has buffered, and pace each one.
		if frame.Type == codec.FrameTypeNone {
			frameBuffer = frameBuffer[consumed:]
			if v.h264Dec != nil {
				for _, videoFrame := range v.h264Dec.DrainFrames() {
					if videoFrame == nil {
						continue
					}
					esc := displayFrame(videoFrame)
					videoFrame.Close()
					videoFrameCount++
					if esc {
						return nil
					}
				}
			}
			continue
		}

		switch frame.Type {
		case codec.FrameTypeVideo:
			if frame.VideoFrame != nil {
				esc := displayFrame(frame.VideoFrame)
				frame.VideoFrame.Close()
				videoFrameCount++
				if esc {
					return nil
				}
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

// fetchBlobsLive subscribes to new blobs and forwards them to out. The
// subscription starts emitting from the current chain head, NOT from
// fromHeight — so any blobs in [fromHeight, firstSubHeight) would be
// lost without intervention. Since the H.264 encoder emits SPS/PPS+IDR
// only every GOPSize frames (~6s), losing the prior IDR means the
// viewer would see a multi-second decode-stall on every live join.
//
// To bridge the gap: capture the first subscription response (so we
// know firstSubHeight), back-fill historical blobs in
// [fromHeight, firstSubHeight) via GetAll, then forward the buffered
// first response, then keep streaming subsequent responses.
func (v *Viewer) fetchBlobsLive(ctx context.Context, fromHeight uint64, out chan<- []byte) {
	defer close(out)

	sub, err := v.client.Blob.Subscribe(ctx, v.namespace)
	if err != nil {
		log.Printf("Fetcher: failed to subscribe to blobs: %v", err)
		return
	}
	log.Println("Fetcher: subscribed to live blobs")

	// Wait for the first subscription response — this tells us where
	// the live tail begins so we can backfill from the entrypoint up
	// to (but not including) it.
	var firstHeight uint64
	var firstData [][]byte
	select {
	case <-ctx.Done():
		return
	case resp, ok := <-sub:
		if !ok {
			log.Println("Fetcher: subscription closed before first response")
			return
		}
		firstHeight = resp.Height
		for _, b := range resp.Blobs {
			if _, _, _, _, _, _, valid := codec.ParseH264Entrypoint(b.Data()); valid {
				continue
			}
			firstData = append(firstData, b.Data())
		}
	}

	blobsSent := 0
	emit := func(height uint64, data []byte) bool {
		select {
		case <-ctx.Done():
			return false
		case out <- data:
			blobsSent++
			log.Printf("Fetcher: sent blob %d (%d bytes) from height %d", blobsSent, len(data), height)
			return true
		}
	}

	// Backfill the gap, if any.
	if firstHeight > fromHeight {
		log.Printf("Fetcher: backfilling heights [%d, %d) before live tail", fromHeight, firstHeight)
		for h := fromHeight; h < firstHeight; h++ {
			select {
			case <-ctx.Done():
				return
			default:
			}
			blobs, err := v.client.Blob.GetAll(ctx, h, []share.Namespace{v.namespace})
			if err != nil || len(blobs) == 0 {
				continue
			}
			for _, b := range blobs {
				if _, _, _, _, _, _, valid := codec.ParseH264Entrypoint(b.Data()); valid {
					continue
				}
				if !emit(h, b.Data()) {
					return
				}
			}
		}
	}

	// Forward the first subscription response we already pulled.
	for _, data := range firstData {
		if !emit(firstHeight, data) {
			return
		}
	}

	// Stream subsequent responses.
	for {
		select {
		case <-ctx.Done():
			return
		case resp, ok := <-sub:
			if !ok {
				log.Println("Fetcher: subscription channel closed")
				return
			}
			for _, b := range resp.Blobs {
				if _, _, _, _, _, _, valid := codec.ParseH264Entrypoint(b.Data()); valid {
					continue
				}
				if !emit(resp.Height, b.Data()) {
					return
				}
			}
		}
	}
}

// fetchBlobs fetches blobs in the background and sends data to channel
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
			// Skip entrypoint blobs
			if _, _, _, _, _, _, valid := codec.ParseH264Entrypoint(b.Data()); valid {
				continue
			}

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
