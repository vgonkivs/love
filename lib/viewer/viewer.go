package viewer

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"time"

	client "github.com/celestiaorg/celestia-openrpc"
	"github.com/celestiaorg/celestia-openrpc/types/share"
	"gocv.io/x/gocv"

	"github.com/vgonkivs/love/lib/audio"
	"github.com/vgonkivs/love/lib/codec"
	"github.com/vgonkivs/love/lib/streamer"
)

// Viewer subscribes to Celestia blobs and plays video in real-time
type Viewer struct {
	cfg       *Config
	client    *client.Client
	namespace share.Namespace
	height    uint64
}

// NewViewer creates a new viewer
func NewViewer(cfg *Config, namespaceHex string, startHeight uint64) (*Viewer, error) {
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

// Run starts the viewer, fetching blobs and displaying frames
func (v *Viewer) Run(ctx context.Context) error {
	if v.client == nil {
		return fmt.Errorf("not connected to Celestia node")
	}

	// Create display window
	window := gocv.NewWindow(v.cfg.WindowName)
	defer window.Close()

	currentHeight := v.height
	buffer := make([]byte, 0)
	frameCount := 0

	log.Printf("Viewer: starting playback from height %d, namespace: %s",
		currentHeight, hex.EncodeToString(v.namespace))

	for {
		select {
		case <-ctx.Done():
			log.Printf("Viewer: stopping, displayed %d frames", frameCount)
			return nil
		default:
		}

		// Fetch blobs at current height
		blobs, err := v.client.Blob.GetAll(ctx, currentHeight, []share.Namespace{v.namespace})
		if err != nil {
			// Height may not exist yet, wait and retry
			time.Sleep(v.cfg.PollDelay)
			continue
		}

		// Process each blob
		for _, b := range blobs {
			buffer = append(buffer, b.Data...)
		}

		// Decode and display frames from buffer
		for {
			frame, consumed := codec.DecodeNextFrame(buffer)
			if frame == nil {
				break
			}

			window.IMShow(*frame)
			key := window.WaitKey(1)
			if key == 27 { // ESC key
				log.Println("Viewer: user closed window")
				frame.Close()
				return nil
			}

			frame.Close()
			buffer = buffer[consumed:]
			frameCount++
		}

		currentHeight++
	}
}

// SequencedBlobData holds blob data with its sequence number
type SequencedBlobData struct {
	Sequence uint64
	Data     []byte
}

// RunWithAudio starts the viewer with audio playback support
// Uses the multiplexed stream format with audio and video frames
// Handles sequenced blobs for proper ordering
func (v *Viewer) RunWithAudio(ctx context.Context) error {
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
			sr, ch, f, valid := streamer.ParseEntrypointBlob(b.Data)
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

	// Setup audio player with detected settings
	audioChannel := make(chan []byte, 100)
	audioCfg := &audio.Config{
		DeviceID:   -1,
		SampleRate: sampleRate,
		Channels:   channels,
		BufferSize: 1024,
	}
	audioPlayer := audio.NewPlayer(audioCfg)

	// Start audio player in goroutine
	audioCtx, audioCancel := context.WithCancel(ctx)
	defer audioCancel()

	go func() {
		if err := audioPlayer.Run(audioCtx, audioChannel); err != nil {
			log.Printf("Audio player error: %v", err)
		}
	}()

	// Buffer for collecting and reordering blobs
	var pendingBlobs []SequencedBlobData
	var nextExpectedSeq uint64 = 1
	frameBuffer := make([]byte, 0)
	videoFrameCount := 0
	audioChunkCount := 0

	// For A/V sync timing
	var playbackStartTime time.Time
	playbackStarted := false

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

		// Parse and collect sequenced blobs
		for _, b := range blobs {
			if len(b.Data) < 8 {
				continue
			}
			// Skip entrypoint blobs
			if _, _, _, valid := streamer.ParseEntrypointBlob(b.Data); valid {
				continue
			}
			seq := binary.LittleEndian.Uint64(b.Data[:8])
			data := b.Data[8:]
			pendingBlobs = append(pendingBlobs, SequencedBlobData{Sequence: seq, Data: data})
		}

		// Sort pending blobs by sequence
		sort.Slice(pendingBlobs, func(i, j int) bool {
			return pendingBlobs[i].Sequence < pendingBlobs[j].Sequence
		})

		// Process blobs in order
		for len(pendingBlobs) > 0 && pendingBlobs[0].Sequence == nextExpectedSeq {
			blob := pendingBlobs[0]
			pendingBlobs = pendingBlobs[1:]
			nextExpectedSeq++

			frameBuffer = append(frameBuffer, blob.Data...)
		}

		// Decode and display frames from buffer
		for {
			frame, consumed := codec.DecodeNextMultiplexedFrame(frameBuffer)
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
				select {
				case audioChannel <- frame.AudioData:
					audioChunkCount++
				default:
				}
			}

			frameBuffer = frameBuffer[consumed:]
		}

		currentHeight++
	}
}
