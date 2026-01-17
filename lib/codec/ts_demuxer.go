package codec

import (
	"bytes"
	"sync"

	"github.com/yapingcat/gomedia/go-mpeg2"
)

// TSDemuxer demuxes MPEG-TS into H.264 video and AAC audio using gomedia (pure Go)
type TSDemuxer struct {
	demuxer *mpeg2.TSDemuxer

	videoChan chan *DemuxedPacket
	audioChan chan *DemuxedPacket
	done      chan struct{}

	width      int
	height     int
	sampleRate int
	channels   int

	started bool
	closed  bool
	closeMu sync.Mutex
}

// DemuxedPacket represents a demuxed video or audio packet
type DemuxedPacket struct {
	Data    []byte
	PTS     uint64 // Presentation timestamp in milliseconds
	DTS     uint64 // Decode timestamp in milliseconds
	IsVideo bool
	IsKey   bool // For video: is this a keyframe?
}

// TSDemuxerConfig holds configuration for the TS demuxer
type TSDemuxerConfig struct {
	Width      int
	Height     int
	SampleRate int
	Channels   int
}

// NewTSDemuxer creates a new MPEG-TS demuxer
func NewTSDemuxer(cfg TSDemuxerConfig) *TSDemuxer {
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 44100
	}
	if cfg.Channels == 0 {
		cfg.Channels = 1
	}

	return &TSDemuxer{
		width:      cfg.Width,
		height:     cfg.Height,
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		videoChan:  make(chan *DemuxedPacket, 30),
		audioChan:  make(chan *DemuxedPacket, 100),
		done:       make(chan struct{}),
	}
}

// Start initializes the demuxer
func (d *TSDemuxer) Start() error {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	if d.started {
		return nil
	}

	d.demuxer = mpeg2.NewTSDemuxer()

	// Set callback for demuxed frames
	d.demuxer.OnFrame = func(cid mpeg2.TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64) {
		// Convert from 90kHz to milliseconds
		ptsMs := pts / 90
		dtsMs := dts / 90

		switch cid {
		case mpeg2.TS_STREAM_H264:
			isKey := isH264KeyframeNAL(frame)
			pkt := &DemuxedPacket{
				Data:    copyBytes(frame),
				PTS:     ptsMs,
				DTS:     dtsMs,
				IsVideo: true,
				IsKey:   isKey,
			}
			select {
			case d.videoChan <- pkt:
			case <-d.done:
			default:
				// Drop frame if channel is full
			}

		case mpeg2.TS_STREAM_AAC:
			pkt := &DemuxedPacket{
				Data:    copyBytes(frame),
				PTS:     ptsMs,
				DTS:     dtsMs,
				IsVideo: false,
			}
			select {
			case d.audioChan <- pkt:
			case <-d.done:
			default:
				// Drop sample if channel is full
			}
		}
	}

	d.started = true
	return nil
}

// copyBytes creates a copy of the byte slice
func copyBytes(data []byte) []byte {
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp
}

// isH264KeyframeNAL checks if H.264 data contains a keyframe
func isH264KeyframeNAL(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// Find NAL unit start
	offset := 0
	if len(data) > 4 && data[0] == 0 && data[1] == 0 {
		if data[2] == 1 {
			offset = 3
		} else if data[2] == 0 && data[3] == 1 {
			offset = 4
		}
	}

	if offset < len(data) {
		nalType := data[offset] & 0x1F
		// IDR slice (type 5) is a keyframe
		return nalType == 5
	}

	return false
}

// WriteMuxedData writes MPEG-TS data to the demuxer for parsing
func (d *TSDemuxer) WriteMuxedData(data []byte) error {
	d.closeMu.Lock()
	if d.closed || !d.started {
		d.closeMu.Unlock()
		return nil
	}
	d.closeMu.Unlock()

	d.demuxer.Input(bytes.NewReader(data))
	return nil
}

// ReadVideoPacket reads a demuxed video packet (non-blocking)
func (d *TSDemuxer) ReadVideoPacket() *DemuxedPacket {
	select {
	case pkt := <-d.videoChan:
		return pkt
	default:
		return nil
	}
}

// ReadAudioPacket reads a demuxed audio packet (non-blocking)
func (d *TSDemuxer) ReadAudioPacket() *DemuxedPacket {
	select {
	case pkt := <-d.audioChan:
		return pkt
	default:
		return nil
	}
}

// Close stops the demuxer
func (d *TSDemuxer) Close() error {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	if d.closed {
		return nil
	}
	d.closed = true

	close(d.done)
	return nil
}
