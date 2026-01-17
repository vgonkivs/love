package codec

import (
	"sync"

	"github.com/yapingcat/gomedia/go-mpeg2"
)

// TSMuxer muxes H.264 video and AAC audio into MPEG-TS using gomedia (pure Go)
type TSMuxer struct {
	muxer    *mpeg2.TSMuxer
	videoPid uint16
	audioPid uint16

	outputBuf []byte
	outputMu  sync.Mutex

	width      int
	height     int
	fps        int
	sampleRate int
	channels   int

	started bool
	closed  bool
	closeMu sync.Mutex
}

// TSMuxerConfig holds configuration for the TS muxer
type TSMuxerConfig struct {
	Width      int
	Height     int
	FPS        int
	SampleRate int
	Channels   int
}

// NewTSMuxer creates a new MPEG-TS muxer
func NewTSMuxer(cfg TSMuxerConfig) *TSMuxer {
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 44100
	}
	if cfg.Channels == 0 {
		cfg.Channels = 1
	}

	return &TSMuxer{
		width:      cfg.Width,
		height:     cfg.Height,
		fps:        cfg.FPS,
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		outputBuf:  make([]byte, 0, 188*100), // Pre-allocate for ~100 TS packets
	}
}

// Start initializes the muxer
func (m *TSMuxer) Start() error {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()

	if m.started {
		return nil
	}

	m.muxer = mpeg2.NewTSMuxer()

	// Add video stream (H.264)
	m.videoPid = m.muxer.AddStream(mpeg2.TS_STREAM_H264)

	// Add audio stream (AAC)
	m.audioPid = m.muxer.AddStream(mpeg2.TS_STREAM_AAC)

	// Set callback for muxed packets
	m.muxer.OnPacket = func(pkg []byte) {
		m.outputMu.Lock()
		m.outputBuf = append(m.outputBuf, pkg...)
		m.outputMu.Unlock()
	}

	m.started = true
	return nil
}

// WriteVideo writes an H.264 NAL unit to the muxer
// pts and dts are in milliseconds
func (m *TSMuxer) WriteVideo(nalUnit []byte, pts, dts uint64) error {
	m.closeMu.Lock()
	if m.closed || !m.started {
		m.closeMu.Unlock()
		return nil
	}
	m.closeMu.Unlock()

	// Write to muxer (pts/dts in 90kHz clock)
	pts90k := pts * 90 // Convert ms to 90kHz
	dts90k := dts * 90

	m.muxer.Write(m.videoPid, nalUnit, pts90k, dts90k)

	return nil
}

// WriteAudio writes AAC audio data to the muxer
// pts is in milliseconds
func (m *TSMuxer) WriteAudio(aacData []byte, pts uint64) error {
	m.closeMu.Lock()
	if m.closed || !m.started {
		m.closeMu.Unlock()
		return nil
	}
	m.closeMu.Unlock()

	// Write to muxer (pts in 90kHz clock)
	pts90k := pts * 90
	m.muxer.Write(m.audioPid, aacData, pts90k, pts90k)

	return nil
}

// ReadMuxedData reads all available muxed MPEG-TS data
func (m *TSMuxer) ReadMuxedData() []byte {
	m.outputMu.Lock()
	defer m.outputMu.Unlock()

	if len(m.outputBuf) == 0 {
		return nil
	}

	data := make([]byte, len(m.outputBuf))
	copy(data, m.outputBuf)
	m.outputBuf = m.outputBuf[:0]

	return data
}

// Close stops the muxer
func (m *TSMuxer) Close() error {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true

	return nil
}
