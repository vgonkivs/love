package codec

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"gocv.io/x/gocv"
)

// H264Encoder encodes video frames to H.264 using ffmpeg. The output
// channel emits raw Annex-B NAL units; callers are expected to package
// them into a container (e.g. MPEG-TS via TSEncoder) and assign their
// own timing.
type H264Encoder struct {
	width   int
	height  int
	fps     int
	bitrate string
	gopSize int // Keyframe interval (frames)

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr bytes.Buffer

	encodedChan chan []byte
	errorChan   chan error
	done        chan struct{}
	started     bool
	closed      bool
	closeMu     sync.Mutex
}

// H264EncoderConfig holds configuration for the H.264 encoder
type H264EncoderConfig struct {
	Width   int
	Height  int
	FPS     int
	Bitrate string // e.g., "2M", "4M"
	GOPSize int    // Keyframe interval in frames (default: fps * 6 for 6-second GOP)
}

// DefaultH264EncoderConfig returns sensible defaults for streaming
func DefaultH264EncoderConfig(width, height, fps int) H264EncoderConfig {
	return H264EncoderConfig{
		Width:   width,
		Height:  height,
		FPS:     fps,
		Bitrate: "2M",
		GOPSize: fps * 6, // Keyframe every 6 seconds (aligned with block time)
	}
}

// NewH264Encoder creates a new H.264 encoder
// Note: Call Start() before encoding frames
func NewH264Encoder(cfg H264EncoderConfig) *H264Encoder {
	if cfg.GOPSize == 0 {
		cfg.GOPSize = cfg.FPS * 6
	}
	if cfg.Bitrate == "" {
		cfg.Bitrate = "2M"
	}

	return &H264Encoder{
		width:       cfg.Width,
		height:      cfg.Height,
		fps:         cfg.FPS,
		bitrate:     cfg.Bitrate,
		gopSize:     cfg.GOPSize,
		encodedChan: make(chan []byte, 30), // Buffer up to 30 encoded frames
		errorChan:   make(chan error, 1),
		done:        make(chan struct{}),
	}
}

// Start initializes the ffmpeg process for encoding
func (e *H264Encoder) Start() error {
	e.closeMu.Lock()
	defer e.closeMu.Unlock()

	if e.started {
		return fmt.Errorf("encoder already started")
	}

	// Build ffmpeg command for H.264 encoding
	// Input: raw BGR24 frames from pipe
	// Output: H.264 Annex B format (with start codes) to pipe
	e.cmd = exec.Command("ffmpeg",
		"-f", "rawvideo",
		"-pixel_format", "bgr24",
		"-video_size", fmt.Sprintf("%dx%d", e.width, e.height),
		"-framerate", fmt.Sprintf("%d", e.fps),
		"-i", "pipe:0",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-g", fmt.Sprintf("%d", e.gopSize),
		"-keyint_min", fmt.Sprintf("%d", e.gopSize),
		"-b:v", e.bitrate,
		"-maxrate", e.bitrate,
		"-bufsize", e.bitrate,
		"-pix_fmt", "yuv420p",
		"-f", "h264",
		"-bsf:v", "dump_extra", // Include SPS/PPS with keyframes
		"pipe:1",
	)

	var err error
	e.stdin, err = e.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	e.stdout, err = e.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	e.cmd.Stderr = &e.stderr

	if err := e.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	e.started = true

	// Start goroutine to read encoded output
	go e.readOutput()

	return nil
}

// readOutput continuously reads encoded H.264 data from ffmpeg
func (e *H264Encoder) readOutput() {
	defer close(e.encodedChan)

	buf := make([]byte, 1024*1024) // 1MB read buffer
	nalBuf := bytes.Buffer{}

	for {
		select {
		case <-e.done:
			return
		default:
		}

		n, err := e.stdout.Read(buf)
		if err != nil {
			if err != io.EOF {
				select {
				case e.errorChan <- err:
				default:
				}
			}
			return
		}

		if n > 0 {
			nalBuf.Write(buf[:n])

			// Extract complete NAL units
			// H.264 Annex B format uses start codes: 0x00000001 or 0x000001
			data := nalBuf.Bytes()
			frames := extractNALUnits(data)

			if len(frames) > 0 {
				// Keep incomplete data in buffer
				lastFrame := frames[len(frames)-1]
				lastEnd := bytes.LastIndex(data, lastFrame) + len(lastFrame)
				nalBuf.Reset()
				if lastEnd < len(data) {
					nalBuf.Write(data[lastEnd:])
				}

				// Send complete frames (drop if channel full to prevent deadlock)
				for _, frame := range frames {
					select {
					case e.encodedChan <- frame:
					case <-e.done:
						return
					default:
						// Channel full, drop frame to prevent blocking ffmpeg
					}
				}
			}
		}
	}
}

// extractNALUnits extracts complete NAL units from H.264 Annex B stream
func extractNALUnits(data []byte) [][]byte {
	var units [][]byte
	startCode3 := []byte{0x00, 0x00, 0x01}
	startCode4 := []byte{0x00, 0x00, 0x00, 0x01}

	// Find all start code positions
	var positions []int
	for i := 0; i < len(data)-3; i++ {
		if bytes.Equal(data[i:i+4], startCode4) {
			positions = append(positions, i)
			i += 3
		} else if bytes.Equal(data[i:i+3], startCode3) {
			positions = append(positions, i)
			i += 2
		}
	}

	// Extract NAL units between start codes
	for i := 0; i < len(positions)-1; i++ {
		start := positions[i]
		end := positions[i+1]
		unit := make([]byte, end-start)
		copy(unit, data[start:end])
		units = append(units, unit)
	}

	return units
}

// DrainRawNALs returns all currently buffered NAL units without any
// framing wrapper. Intended for callers that mux the NALs into a
// container (e.g. MPEG-TS) and apply their own timestamping.
func (e *H264Encoder) DrainRawNALs() [][]byte {
	var nals [][]byte
	for {
		select {
		case nal, ok := <-e.encodedChan:
			if !ok {
				return nals
			}
			nals = append(nals, nal)
		default:
			return nals
		}
	}
}

// WriteFrame feeds a raw video frame to the encoder without draining or
// wrapping the output. Pair with DrainRawNALs for muxed pipelines.
func (e *H264Encoder) WriteFrame(frame gocv.Mat) error {
	e.closeMu.Lock()
	if e.closed {
		e.closeMu.Unlock()
		return fmt.Errorf("encoder is closed")
	}
	if !e.started {
		e.closeMu.Unlock()
		return fmt.Errorf("encoder not started, call Start() first")
	}
	e.closeMu.Unlock()

	data := frame.ToBytes()
	expectedSize := e.width * e.height * 3
	if len(data) != expectedSize {
		return fmt.Errorf("frame size mismatch: got %d, expected %d", len(data), expectedSize)
	}

	if _, err := e.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write frame: %w", err)
	}
	return nil
}

// Close stops the encoder and releases resources
func (e *H264Encoder) Close() error {
	e.closeMu.Lock()
	defer e.closeMu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	close(e.done)

	if e.stdin != nil {
		e.stdin.Close()
	}

	if e.cmd != nil && e.cmd.Process != nil {
		e.cmd.Process.Kill()
		e.cmd.Wait()
	}

	return nil
}

// GetStderr returns ffmpeg stderr output for debugging
func (e *H264Encoder) GetStderr() string {
	return e.stderr.String()
}

