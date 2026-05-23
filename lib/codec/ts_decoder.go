package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
)

// splitNALUnits splits an Annex-B H.264 byte stream into individual NAL
// units, each prefixed with its original 3- or 4-byte start code. The
// H264 decoder's IDR gate examines only the first NAL per call, so
// access units with multiple NALs (the SPS+PPS+IDR keyframe case) must
// be fed one NAL at a time or the gate caches SPS without arming and
// drops every subsequent slice.
func splitNALUnits(data []byte) [][]byte {
	var (
		startCode3 = []byte{0x00, 0x00, 0x01}
		startCode4 = []byte{0x00, 0x00, 0x00, 0x01}
		positions  []int
	)
	for i := 0; i+3 < len(data); {
		if bytes.Equal(data[i:i+4], startCode4) {
			positions = append(positions, i)
			i += 4
			continue
		}
		if bytes.Equal(data[i:i+3], startCode3) {
			positions = append(positions, i)
			i += 3
			continue
		}
		i++
	}
	if len(positions) == 0 {
		return nil
	}
	nals := make([][]byte, 0, len(positions))
	for i, start := range positions {
		end := len(data)
		if i+1 < len(positions) {
			end = positions[i+1]
		}
		nals = append(nals, data[start:end])
	}
	return nals
}

// TSDecoder demuxes an MPEG-TS byte stream into decoded video and audio
// frames. It implements codec.Decoder so the viewer's playback loop is
// unchanged from the JPEG / raw-H.264 paths.
//
// Decode() consumes all of the supplied bytes into the demuxer, drains
// every available access unit through the H.264 and AAC decoders, and
// returns one decoded frame per call. Pending frames are queued until
// the caller drains them with successive Decode() calls — see the
// loop in viewer.Run for the contract.
type TSDecoder struct {
	width, height       int
	sampleRate, channels int

	demuxer *TSDemuxer
	h264    *H264Decoder
	aac     *AACDecoder

	// Queue of frames ready to be returned by Decode. Populated
	// whenever the demuxer/decoders emit output; drained head-first.
	queue []*DecodedFrame
	// FIFO of PTS values from video packets fed to ffmpeg, used to
	// pair the asynchronously-emitted mats with the PTS of the access
	// unit that produced them. libx264 ultrafast+zerolatency preserves
	// input order, so a simple FIFO is sufficient.
	videoPTSMs []uint64

	startMu sync.Mutex
	started bool
	closed  bool
}

// TSDecoderConfig configures the muxed-TS decoder.
type TSDecoderConfig struct {
	Width, Height int
	SampleRate    int
	Channels      int
}

// NewTSDecoder constructs an unstarted TSDecoder. Call Start before use.
func NewTSDecoder(cfg TSDecoderConfig) *TSDecoder {
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 44100
	}
	if cfg.Channels == 0 {
		cfg.Channels = 1
	}
	return &TSDecoder{
		width:      cfg.Width,
		height:     cfg.Height,
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
	}
}

// Start brings up the underlying demuxer, H.264 decoder, and AAC decoder.
func (d *TSDecoder) Start() error {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	if d.started {
		return nil
	}

	d.demuxer = NewTSDemuxer(TSDemuxerConfig{
		Width:      d.width,
		Height:     d.height,
		SampleRate: d.sampleRate,
		Channels:   d.channels,
	})
	if err := d.demuxer.Start(); err != nil {
		return fmt.Errorf("ts demuxer: %w", err)
	}

	d.h264 = NewH264Decoder(H264DecoderConfig{Width: d.width, Height: d.height})
	if err := d.h264.Start(); err != nil {
		d.demuxer.Close()
		return fmt.Errorf("h264 decoder: %w", err)
	}

	d.aac = NewAACDecoder(AACDecoderConfig{SampleRate: d.sampleRate, Channels: d.channels})
	if err := d.aac.Start(); err != nil {
		d.h264.Close()
		d.demuxer.Close()
		return fmt.Errorf("aac decoder: %w", err)
	}

	d.started = true
	return nil
}

// Close shuts down all subcomponents.
func (d *TSDecoder) Close() error {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	if d.aac != nil {
		d.aac.Close()
	}
	if d.h264 != nil {
		d.h264.Close()
	}
	if d.demuxer != nil {
		d.demuxer.Close()
	}
	return nil
}

// Decode feeds bytes to the demuxer, drains every available frame, and
// returns the head of the internal frame queue. The returned int is the
// number of bytes consumed from the input — always len(data), since the
// demuxer absorbs whatever it is given.
//
// When the queue is empty after draining, Decode returns (nil, len(data)).
// The viewer's playback loop treats a nil frame with a non-zero consumed
// count as "slide the buffer forward and keep going" — exactly the
// behavior we want when a packet was fed but no frame is ready yet.
func (d *TSDecoder) Decode(data []byte) (*DecodedFrame, int) {
	consumed := len(data)

	if !d.isStarted() {
		return nil, consumed
	}

	if consumed > 0 {
		_ = d.demuxer.WriteMuxedData(data)
	}

	d.drain()

	if len(d.queue) > 0 {
		head := d.queue[0]
		d.queue = d.queue[1:]
		return head, consumed
	}
	return nil, consumed
}

// drain pulls every available packet out of the demuxer, pushes video
// NALs and audio frames to their respective decoders, and queues any
// resulting frames. Called every Decode() to keep the pipeline moving.
func (d *TSDecoder) drain() {
	for {
		pkt := d.demuxer.ReadVideoPacket()
		if pkt == nil {
			break
		}
		// gomedia delivers one full H.264 access unit per packet, so a
		// keyframe arrives as SPS+PPS+IDR concatenated. The H264 decoder's
		// IDR gate inspects only the first NAL in each call, so feeding
		// the whole packet would cache SPS without ever flipping the
		// "primed" flag and every subsequent P-slice would be dropped.
		// Split the access unit into individual NALs first.
		nals := splitNALUnits(pkt.Data)
		if len(nals) == 0 {
			continue
		}
		// One PTS entry per access unit, not per NAL — DrainFrames
		// emits one mat per access unit.
		d.videoPTSMs = append(d.videoPTSMs, pkt.PTS)
		for _, nal := range nals {
			// Ignore the immediate return; mats come out of DrainFrames
			// below once ffmpeg has buffered enough to emit one.
			_, _ = d.h264.DecodeH264Frame(nal)
		}
	}

	for {
		pkt := d.demuxer.ReadAudioPacket()
		if pkt == nil {
			break
		}
		if err := d.aac.Write(pkt.Data); err != nil {
			continue
		}
		for {
			pcm := d.aac.ReadSamples()
			if pcm == nil {
				break
			}
			d.queue = append(d.queue, &DecodedFrame{
				Type:      FrameTypeAudio,
				AudioData: pcm,
				Timestamp: pkt.PTS * uint64(1_000_000), // ms → ns
			})
		}
	}

	for _, mat := range d.h264.DrainFrames() {
		var pts uint64
		if len(d.videoPTSMs) > 0 {
			pts = d.videoPTSMs[0]
			d.videoPTSMs = d.videoPTSMs[1:]
		}
		d.queue = append(d.queue, &DecodedFrame{
			Type:       FrameTypeVideo,
			VideoFrame: mat,
			Timestamp:  pts * uint64(1_000_000), // ms → ns
		})
	}
}

// ParseEntrypoint implements codec.Decoder. Returns sample rate, channels,
// fps from the ENTR blob.
func (d *TSDecoder) ParseEntrypoint(data []byte) (sampleRate, channels, fps int, valid bool) {
	sr, ch, f, _, _, _, ok := ParseH264Entrypoint(data)
	return sr, ch, f, ok
}

// ParseTSEntrypoint reports width and height in addition to the standard
// fields, mirroring ParseH264Entrypoint for callers that have the full
// 15-byte blob.
func ParseTSEntrypoint(data []byte) (sampleRate, channels, fps, width, height int, valid bool) {
	if len(data) < 15 {
		return 0, 0, 0, 0, 0, false
	}
	if string(data[:4]) != string(EntrypointMarker) {
		return 0, 0, 0, 0, 0, false
	}
	if data[10] != CodecIDTS {
		return 0, 0, 0, 0, 0, false
	}
	sampleRate = int(binary.LittleEndian.Uint32(data[4:8]))
	channels = int(data[8])
	fps = int(data[9])
	width = int(binary.LittleEndian.Uint16(data[11:13]))
	height = int(binary.LittleEndian.Uint16(data[13:15]))
	return sampleRate, channels, fps, width, height, true
}

func (d *TSDecoder) isStarted() bool {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	return d.started && !d.closed
}
