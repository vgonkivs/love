package codec

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"gocv.io/x/gocv"
)

// TSEncoder produces an MPEG-TS byte stream of H.264 video and AAC audio.
// It implements codec.Encoder so it can be plugged into the capturer in
// place of the legacy raw-H.264 wrapper. PTS/DTS flow through the muxer
// per access unit, which is what callers like R2 need for A/V sync.
type TSEncoder struct {
	width, height, fps     int
	sampleRate, channels   int
	bitrate                string
	audioBitrate           string

	h264 *H264Encoder
	aac  *AACEncoder
	mux  *TSMuxer

	// muxMu serializes WriteVideo/WriteAudio on the muxer: gomedia's
	// TSMuxer.Write is not goroutine-safe and the capturer drives video
	// from the ticker loop while audio fires from the malgo callback.
	muxMu sync.Mutex

	startMu sync.Mutex
	started bool
	closed  bool
}

// TSEncoderConfig configures the muxed-TS encoder.
type TSEncoderConfig struct {
	Width, Height int
	FPS           int
	Bitrate       string // video bitrate, e.g. "2M"
	SampleRate    int
	Channels      int
	AudioBitrate  string // AAC bitrate, e.g. "128k"
}

// NewTSEncoder constructs an unstarted TSEncoder. Call Start before use.
func NewTSEncoder(cfg TSEncoderConfig) *TSEncoder {
	if cfg.Bitrate == "" {
		cfg.Bitrate = "2M"
	}
	if cfg.AudioBitrate == "" {
		cfg.AudioBitrate = "128k"
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 44100
	}
	if cfg.Channels == 0 {
		cfg.Channels = 1
	}

	return &TSEncoder{
		width:        cfg.Width,
		height:       cfg.Height,
		fps:          cfg.FPS,
		sampleRate:   cfg.SampleRate,
		channels:     cfg.Channels,
		bitrate:      cfg.Bitrate,
		audioBitrate: cfg.AudioBitrate,
	}
}

// Start brings up the underlying H.264 encoder, AAC encoder, and TS muxer.
// Safe to call once; subsequent calls are no-ops.
func (e *TSEncoder) Start() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	if e.started {
		return nil
	}

	e.h264 = NewH264Encoder(H264EncoderConfig{
		Width:   e.width,
		Height:  e.height,
		FPS:     e.fps,
		Bitrate: e.bitrate,
		GOPSize: e.fps * 6,
	})
	if err := e.h264.Start(); err != nil {
		return fmt.Errorf("h264 encoder: %w", err)
	}

	e.aac = NewAACEncoder(AACEncoderConfig{
		SampleRate: e.sampleRate,
		Channels:   e.channels,
		Bitrate:    e.audioBitrate,
	})
	if err := e.aac.Start(); err != nil {
		e.h264.Close()
		return fmt.Errorf("aac encoder: %w", err)
	}

	e.mux = NewTSMuxer(TSMuxerConfig{
		Width:      e.width,
		Height:     e.height,
		FPS:        e.fps,
		SampleRate: e.sampleRate,
		Channels:   e.channels,
	})
	if err := e.mux.Start(); err != nil {
		e.aac.Close()
		e.h264.Close()
		return fmt.Errorf("ts muxer: %w", err)
	}

	e.started = true
	return nil
}

// Close shuts down all subcomponents.
func (e *TSEncoder) Close() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	if e.mux != nil {
		e.mux.Close()
	}
	if e.aac != nil {
		e.aac.Close()
	}
	if e.h264 != nil {
		e.h264.Close()
	}
	return nil
}

// EncodeVideo feeds a raw BGR frame to the encoder pipeline and returns
// whatever MPEG-TS bytes the muxer has ready. PTS/DTS are derived from
// the supplied timestamp (milliseconds since stream start).
//
// The returned slice may be empty when the H.264 encoder is still
// buffering — the caller should treat empty output as "no chunk yet"
// and continue feeding frames. The sequence argument is unused; the
// muxer carries its own timing.
func (e *TSEncoder) EncodeVideo(frame gocv.Mat, timestamp time.Duration, _ uint32) ([]byte, error) {
	if !e.isStarted() {
		return nil, fmt.Errorf("ts encoder not started, call Start() first")
	}

	if err := e.h264.WriteFrame(frame); err != nil {
		return nil, fmt.Errorf("write frame: %w", err)
	}

	pts := uint64(timestamp.Milliseconds())
	nals := e.h264.DrainRawNALs()

	e.muxMu.Lock()
	for _, nal := range nals {
		if err := e.mux.WriteVideo(nal, pts, pts); err != nil {
			e.muxMu.Unlock()
			return nil, fmt.Errorf("mux video: %w", err)
		}
	}
	e.muxMu.Unlock()

	return e.mux.ReadMuxedData(), nil
}

// EncodeAudio feeds PCM samples to the AAC encoder, then muxes any
// resulting ADTS frames into the TS stream. Returns the muxer's
// currently-buffered bytes (may be empty).
func (e *TSEncoder) EncodeAudio(samples []byte, timestamp time.Duration, _ uint32) ([]byte, error) {
	if !e.isStarted() {
		return nil, fmt.Errorf("ts encoder not started, call Start() first")
	}

	if err := e.aac.Write(samples); err != nil {
		return nil, fmt.Errorf("write samples: %w", err)
	}

	pts := uint64(timestamp.Milliseconds())

	e.muxMu.Lock()
	defer e.muxMu.Unlock()
	for {
		frame := e.aac.ReadFrame()
		if frame == nil {
			break
		}
		if err := e.mux.WriteAudio(frame, pts); err != nil {
			return nil, fmt.Errorf("mux audio: %w", err)
		}
	}

	return e.mux.ReadMuxedData(), nil
}

// CreateEntrypoint emits the standard 15-byte ENTR blob tagged with
// CodecIDTS so the viewer constructs a TSDecoder.
func (e *TSEncoder) CreateEntrypoint(sampleRate, channels, fps int) []byte {
	data := make([]byte, 15)
	copy(data[:4], EntrypointMarker)
	binary.LittleEndian.PutUint32(data[4:8], uint32(sampleRate))
	data[8] = byte(channels)
	data[9] = byte(fps)
	data[10] = CodecIDTS
	binary.LittleEndian.PutUint16(data[11:13], uint16(e.width))
	binary.LittleEndian.PutUint16(data[13:15], uint16(e.height))
	return data
}

// CreateStreamEnd emits the ENDS blob with total duration and frame count.
func (e *TSEncoder) CreateStreamEnd(totalDuration time.Duration, totalFrames uint32) []byte {
	data := make([]byte, 16)
	copy(data[:4], StreamEndMarker)
	binary.LittleEndian.PutUint64(data[4:12], uint64(totalDuration.Nanoseconds()))
	binary.LittleEndian.PutUint32(data[12:16], totalFrames)
	return data
}

func (e *TSEncoder) isStarted() bool {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	return e.started && !e.closed
}
