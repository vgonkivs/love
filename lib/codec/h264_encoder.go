package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"gocv.io/x/gocv"
)

// H264FrameMarker identifies H.264 encoded video frames
var H264FrameMarker = []byte{'H', '2', '6', '4'}

// H264Encoder encodes video frames to H.264 using ffmpeg
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

	outputBuf    bytes.Buffer
	outputMu     sync.Mutex
	encodedChan  chan []byte
	errorChan    chan error
	done         chan struct{}
	started      bool
	frameCount   uint32
	closed       bool
	closeMu      sync.Mutex
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

				// Send complete frames
				for _, frame := range frames {
					select {
					case e.encodedChan <- frame:
					case <-e.done:
						return
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

// EncodeVideo encodes a video frame to H.264
// Returns the encoded frame with header, or nil if frame is still being processed
// Call ReadEncodedFrame() to get encoded output asynchronously
func (e *H264Encoder) EncodeVideo(frame gocv.Mat, timestamp time.Duration, sequence uint32) ([]byte, error) {
	e.closeMu.Lock()
	if e.closed {
		e.closeMu.Unlock()
		return nil, fmt.Errorf("encoder is closed")
	}
	if !e.started {
		e.closeMu.Unlock()
		return nil, fmt.Errorf("encoder not started, call Start() first")
	}
	e.closeMu.Unlock()

	// Get raw frame data (BGR24)
	data := frame.ToBytes()
	expectedSize := e.width * e.height * 3
	if len(data) != expectedSize {
		return nil, fmt.Errorf("frame size mismatch: got %d, expected %d", len(data), expectedSize)
	}

	// Write frame to ffmpeg stdin
	_, err := e.stdin.Write(data)
	if err != nil {
		return nil, fmt.Errorf("failed to write frame: %w", err)
	}

	e.frameCount++

	// Try to read an encoded frame (non-blocking)
	select {
	case encoded, ok := <-e.encodedChan:
		if !ok {
			return nil, fmt.Errorf("encoder channel closed")
		}
		return e.wrapFrame(encoded, timestamp, sequence), nil
	default:
		// Frame is being processed, return nil (caller should use ReadEncodedFrame)
		return nil, nil
	}
}

// ReadEncodedFrameTimeout reads with timeout
func (e *H264Encoder) ReadEncodedFrameTimeout(timestamp time.Duration, sequence uint32, timeout time.Duration) ([]byte, error) {
	select {
	case encoded, ok := <-e.encodedChan:
		if !ok {
			return nil, io.EOF
		}
		return e.wrapFrame(encoded, timestamp, sequence), nil
	case err := <-e.errorChan:
		return nil, err
	case <-e.done:
		return nil, io.EOF
	case <-time.After(timeout):
		return nil, nil // No frame available yet
	}
}

// wrapFrame wraps encoded H.264 data with our frame header
func (e *H264Encoder) wrapFrame(encoded []byte, timestamp time.Duration, sequence uint32) []byte {
	header := make([]byte, FrameHeaderSize)
	copy(header[:4], H264FrameMarker)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(encoded)))
	binary.LittleEndian.PutUint64(header[8:16], uint64(timestamp.Nanoseconds()))
	binary.LittleEndian.PutUint32(header[16:20], sequence)

	result := make([]byte, len(header)+len(encoded))
	copy(result, header)
	copy(result[len(header):], encoded)
	return result
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

// EncodeAudio encodes audio samples with header (same as JPEG codec)
func (e *H264Encoder) EncodeAudio(samples []byte, timestamp time.Duration, sequence uint32) ([]byte, error) {
	header := EncodeFrameHeaderWithTimestamp(AudioFrameMarker, len(samples), uint64(timestamp.Nanoseconds()), sequence)

	result := make([]byte, len(header)+len(samples))
	copy(result, header)
	copy(result[len(header):], samples)

	return result, nil
}

// CreateEntrypoint creates the metadata blob for stream start
// Format: ENTR (4 bytes) + sample_rate (4 bytes) + channels (1 byte) + fps (1 byte) + codec (1 byte) + width (2 bytes) + height (2 bytes)
// Codec: 0 = JPEG, 1 = H.264
func (e *H264Encoder) CreateEntrypoint(sampleRate int, channels int, fps int) []byte {
	data := make([]byte, 15) // Extended format with codec and dimensions
	copy(data[:4], EntrypointMarker)
	binary.LittleEndian.PutUint32(data[4:8], uint32(sampleRate))
	data[8] = byte(channels)
	data[9] = byte(fps)
	data[10] = 1 // H.264 codec identifier
	binary.LittleEndian.PutUint16(data[11:13], uint16(e.width))
	binary.LittleEndian.PutUint16(data[13:15], uint16(e.height))
	return data
}

// CreateStreamEnd creates the stream end notification blob
// Format: ENDS (4 bytes) + total_duration_ns (8 bytes) + total_frames (4 bytes)
func (e *H264Encoder) CreateStreamEnd(totalDuration time.Duration, totalFrames uint32) []byte {
	data := make([]byte, 16)
	copy(data[:4], StreamEndMarker)
	binary.LittleEndian.PutUint64(data[4:12], uint64(totalDuration.Nanoseconds()))
	binary.LittleEndian.PutUint32(data[12:16], totalFrames)
	return data
}
