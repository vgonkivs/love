package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"gocv.io/x/gocv"
)

// H264Decoder decodes H.264 video frames using ffmpeg
type H264Decoder struct {
	width  int
	height int

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr bytes.Buffer

	frameSize   int
	decodedChan chan *gocv.Mat
	errorChan   chan error
	done        chan struct{}
	started     bool
	closed      bool
	closeMu     sync.Mutex

	// Cached SPS/PPS for mid-stream joining
	sps     []byte
	pps     []byte
	spsPps  []byte // Combined SPS+PPS to prepend
	spsMu   sync.Mutex
	hasSPS  bool
}

// H264DecoderConfig holds configuration for the H.264 decoder
type H264DecoderConfig struct {
	Width  int
	Height int
}

// NewH264Decoder creates a new H.264 decoder
// Note: Call Start() before decoding frames
func NewH264Decoder(cfg H264DecoderConfig) *H264Decoder {
	return &H264Decoder{
		width:       cfg.Width,
		height:      cfg.Height,
		frameSize:   cfg.Width * cfg.Height * 3, // BGR24
		decodedChan: make(chan *gocv.Mat, 30),   // Buffer up to 30 decoded frames
		errorChan:   make(chan error, 1),
		done:        make(chan struct{}),
	}
}

// Start initializes the ffmpeg process for decoding
func (d *H264Decoder) Start() error {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	if d.started {
		return fmt.Errorf("decoder already started")
	}

	// Build ffmpeg command for H.264 decoding
	// Input: H.264 Annex B format from pipe
	// Output: raw BGR24 frames to pipe
	d.cmd = exec.Command("ffmpeg",
		"-f", "h264",
		"-i", "pipe:0",
		"-f", "rawvideo",
		"-pix_fmt", "bgr24",
		"-video_size", fmt.Sprintf("%dx%d", d.width, d.height),
		"pipe:1",
	)

	var err error
	d.stdin, err = d.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	d.stdout, err = d.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	d.cmd.Stderr = &d.stderr

	if err := d.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	d.started = true

	// Start goroutine to read decoded output
	go d.readOutput()

	return nil
}

// readOutput continuously reads decoded frames from ffmpeg
func (d *H264Decoder) readOutput() {
	defer close(d.decodedChan)

	frameBuf := make([]byte, d.frameSize)

	for {
		select {
		case <-d.done:
			return
		default:
		}

		// Read exactly one frame worth of data
		n, err := io.ReadFull(d.stdout, frameBuf)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				select {
				case d.errorChan <- err:
				default:
				}
			}
			return
		}

		if n == d.frameSize {
			// Create gocv.Mat from raw BGR24 data
			mat, err := gocv.NewMatFromBytes(d.height, d.width, gocv.MatTypeCV8UC3, frameBuf)
			if err != nil {
				select {
				case d.errorChan <- fmt.Errorf("failed to create Mat: %w", err):
				default:
				}
				continue
			}

			// Clone the mat since we reuse the buffer
			clone := mat.Clone()
			mat.Close()

			select {
			case d.decodedChan <- &clone:
			case <-d.done:
				clone.Close()
				return
			}
		}
	}
}

// getNALType returns the NAL unit type from H.264 data (handles both 3 and 4 byte start codes)
func getNALType(data []byte) int {
	if len(data) < 5 {
		return -1
	}
	// Check for 4-byte start code (0x00000001)
	if data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 1 {
		return int(data[4] & 0x1F)
	}
	// Check for 3-byte start code (0x000001)
	if data[0] == 0 && data[1] == 0 && data[2] == 1 {
		return int(data[3] & 0x1F)
	}
	return -1
}

// NAL unit types
const (
	nalTypeSPS = 7  // Sequence Parameter Set
	nalTypePPS = 8  // Picture Parameter Set
	nalTypeIDR = 5  // IDR frame (keyframe)
)

// DecodeH264Frame decodes raw H.264 NAL unit data (without our header)
// This feeds the H.264 data to ffmpeg and returns any available decoded frame
func (d *H264Decoder) DecodeH264Frame(h264Data []byte) (*gocv.Mat, error) {
	d.closeMu.Lock()
	if d.closed {
		d.closeMu.Unlock()
		return nil, fmt.Errorf("decoder is closed")
	}
	if !d.started {
		d.closeMu.Unlock()
		return nil, fmt.Errorf("decoder not started, call Start() first")
	}
	d.closeMu.Unlock()

	// Check NAL type and cache SPS/PPS
	nalType := getNALType(h264Data)

	d.spsMu.Lock()
	switch nalType {
	case nalTypeSPS:
		d.sps = make([]byte, len(h264Data))
		copy(d.sps, h264Data)
		d.updateSpsPps()
	case nalTypePPS:
		d.pps = make([]byte, len(h264Data))
		copy(d.pps, h264Data)
		d.updateSpsPps()
	case nalTypeIDR:
		// For IDR frames, prepend SPS/PPS if we have them and haven't sent them yet
		if len(d.spsPps) > 0 && !d.hasSPS {
			_, err := d.stdin.Write(d.spsPps)
			if err != nil {
				d.spsMu.Unlock()
				return nil, fmt.Errorf("failed to write SPS/PPS: %w", err)
			}
			d.hasSPS = true
		}
	}
	d.spsMu.Unlock()

	// Write H.264 data to ffmpeg stdin
	_, err := d.stdin.Write(h264Data)
	if err != nil {
		return nil, fmt.Errorf("failed to write H.264 data: %w", err)
	}

	// Try to read a decoded frame (non-blocking)
	select {
	case frame, ok := <-d.decodedChan:
		if !ok {
			return nil, fmt.Errorf("decoder channel closed")
		}
		return frame, nil
	default:
		// Frame is being processed
		return nil, nil
	}
}

// updateSpsPps combines SPS and PPS into a single buffer
func (d *H264Decoder) updateSpsPps() {
	if len(d.sps) > 0 && len(d.pps) > 0 {
		d.spsPps = make([]byte, len(d.sps)+len(d.pps))
		copy(d.spsPps, d.sps)
		copy(d.spsPps[len(d.sps):], d.pps)
	}
}

// Decode parses H.264 frame from multiplexed data with our header format
// Returns the decoded frame info and bytes consumed
// Note: This only extracts the frame data; actual H.264 decoding requires
// feeding the data to DecodeH264Frame()
func (d *H264Decoder) Decode(data []byte) (*DecodedFrame, int) {
	if len(data) < 8 {
		return nil, 0
	}

	marker := data[:4]
	isH264 := bytes.Equal(marker, H264FrameMarker)
	isAudio := bytes.Equal(marker, AudioFrameMarker)

	if !isH264 && !isAudio {
		return nil, 1 // Skip unknown marker
	}

	frameSize := int(binary.LittleEndian.Uint32(data[4:8]))

	var headerSize int
	var timestamp uint64
	var sequence uint32

	if len(data) >= FrameHeaderSize {
		if FrameHeaderSize+frameSize <= len(data) {
			headerSize = FrameHeaderSize
			timestamp = binary.LittleEndian.Uint64(data[8:16])
			sequence = binary.LittleEndian.Uint32(data[16:20])
		} else if 8+frameSize <= len(data) {
			headerSize = 8
		} else {
			return nil, 0
		}
	} else if 8+frameSize <= len(data) {
		headerSize = 8
	} else {
		return nil, 0
	}

	totalSize := headerSize + frameSize
	if len(data) < totalSize {
		return nil, 0
	}

	frameData := data[headerSize:totalSize]

	if isH264 {
		// Feed to decoder and try to get a frame
		frame, err := d.DecodeH264Frame(frameData)
		if err != nil {
			return nil, totalSize
		}

		if frame != nil {
			return &DecodedFrame{
				Type:       FrameTypeVideo,
				VideoFrame: frame,
				Timestamp:  timestamp,
				Sequence:   sequence,
			}, totalSize
		}

		// Frame fed to decoder but no output yet (normal for H.264)
		// Return a marker indicating data was consumed but no frame ready
		return &DecodedFrame{
			Type:      FrameTypeNone,
			Timestamp: timestamp,
			Sequence:  sequence,
		}, totalSize
	}

	if isAudio {
		audioCopy := make([]byte, len(frameData))
		copy(audioCopy, frameData)
		return &DecodedFrame{
			Type:      FrameTypeAudio,
			AudioData: audioCopy,
			Timestamp: timestamp,
			Sequence:  sequence,
		}, totalSize
	}

	return nil, 1
}

// Close stops the decoder and releases resources
func (d *H264Decoder) Close() error {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	if d.closed {
		return nil
	}
	d.closed = true

	close(d.done)

	if d.stdin != nil {
		d.stdin.Close()
	}

	if d.cmd != nil && d.cmd.Process != nil {
		d.cmd.Process.Kill()
		d.cmd.Wait()
	}

	return nil
}

// GetStderr returns ffmpeg stderr output for debugging
func (d *H264Decoder) GetStderr() string {
	return d.stderr.String()
}

// DrainFrames returns all currently available decoded frames (non-blocking)
func (d *H264Decoder) DrainFrames() []*gocv.Mat {
	var frames []*gocv.Mat
	for {
		select {
		case frame, ok := <-d.decodedChan:
			if !ok {
				return frames
			}
			frames = append(frames, frame)
		default:
			return frames
		}
	}
}

// ParseEntrypoint implements the Decoder interface
// Uses the extended H.264 entrypoint format
func (d *H264Decoder) ParseEntrypoint(data []byte) (sampleRate int, channels int, fps int, valid bool) {
	sr, ch, f, _, _, _, v := ParseH264Entrypoint(data)
	return sr, ch, f, v
}

// ParseH264Entrypoint extracts metadata from H.264 entrypoint blob
// Extended format includes codec identifier and dimensions
func ParseH264Entrypoint(data []byte) (sampleRate int, channels int, fps int, width int, height int, isH264 bool, valid bool) {
	if len(data) < 10 {
		return 0, 0, 0, 0, 0, false, false
	}
	if !bytes.Equal(data[:4], EntrypointMarker) {
		return 0, 0, 0, 0, 0, false, false
	}
	sampleRate = int(binary.LittleEndian.Uint32(data[4:8]))
	channels = int(data[8])
	fps = int(data[9])

	// Check for codec identifier (extended format)
	if len(data) >= 11 {
		isH264 = data[10] == 1
	}

	// Check for dimensions (H.264 extended format)
	if len(data) >= 15 {
		width = int(binary.LittleEndian.Uint16(data[11:13]))
		height = int(binary.LittleEndian.Uint16(data[13:15]))
	}

	return sampleRate, channels, fps, width, height, isH264, true
}
