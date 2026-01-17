package codec

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// AACEncoder encodes raw PCM audio to AAC using ffmpeg
type AACEncoder struct {
	sampleRate int
	channels   int
	bitrate    string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr bytes.Buffer

	outputChan chan []byte
	done       chan struct{}
	started    bool
	closed     bool
	closeMu    sync.Mutex
}

// AACEncoderConfig holds configuration for the AAC encoder
type AACEncoderConfig struct {
	SampleRate int
	Channels   int
	Bitrate    string // e.g., "128k"
}

// NewAACEncoder creates a new AAC encoder
func NewAACEncoder(cfg AACEncoderConfig) *AACEncoder {
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 44100
	}
	if cfg.Channels == 0 {
		cfg.Channels = 1
	}
	if cfg.Bitrate == "" {
		cfg.Bitrate = "128k"
	}

	return &AACEncoder{
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		bitrate:    cfg.Bitrate,
		outputChan: make(chan []byte, 100),
		done:       make(chan struct{}),
	}
}

// Start initializes the ffmpeg process for AAC encoding
func (e *AACEncoder) Start() error {
	e.closeMu.Lock()
	defer e.closeMu.Unlock()

	if e.started {
		return fmt.Errorf("encoder already started")
	}

	// Build ffmpeg command for AAC encoding
	// Input: raw S16LE PCM from pipe
	// Output: AAC ADTS to pipe
	e.cmd = exec.Command("ffmpeg",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", e.sampleRate),
		"-ac", fmt.Sprintf("%d", e.channels),
		"-i", "pipe:0",
		"-c:a", "aac",
		"-b:a", e.bitrate,
		"-f", "adts",
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

// readOutput continuously reads encoded AAC data from ffmpeg
func (e *AACEncoder) readOutput() {
	defer close(e.outputChan)

	buf := make([]byte, 8192)
	aacBuf := bytes.Buffer{}

	for {
		select {
		case <-e.done:
			return
		default:
		}

		n, err := e.stdout.Read(buf)
		if err != nil {
			return
		}

		if n > 0 {
			aacBuf.Write(buf[:n])

			// Extract complete ADTS frames
			frames := extractADTSFrames(aacBuf.Bytes())
			if len(frames) > 0 {
				// Keep incomplete data
				lastFrame := frames[len(frames)-1]
				data := aacBuf.Bytes()
				lastEnd := bytes.LastIndex(data, lastFrame) + len(lastFrame)
				aacBuf.Reset()
				if lastEnd < len(data) {
					aacBuf.Write(data[lastEnd:])
				}

				// Send complete frames
				for _, frame := range frames {
					select {
					case e.outputChan <- frame:
					case <-e.done:
						return
					}
				}
			}
		}
	}
}

// extractADTSFrames extracts complete ADTS frames from buffer
func extractADTSFrames(data []byte) [][]byte {
	var frames [][]byte

	for i := 0; i < len(data)-7; {
		// ADTS sync word: 0xFFF
		if data[i] == 0xFF && (data[i+1]&0xF0) == 0xF0 {
			// Get frame length from ADTS header
			frameLen := int(data[i+3]&0x03)<<11 | int(data[i+4])<<3 | int(data[i+5]&0xE0)>>5

			if frameLen > 0 && i+frameLen <= len(data) {
				frame := make([]byte, frameLen)
				copy(frame, data[i:i+frameLen])
				frames = append(frames, frame)
				i += frameLen
			} else {
				// Incomplete frame
				break
			}
		} else {
			i++
		}
	}

	return frames
}

// Write writes raw PCM samples to the encoder
func (e *AACEncoder) Write(samples []byte) error {
	e.closeMu.Lock()
	if e.closed || !e.started {
		e.closeMu.Unlock()
		return fmt.Errorf("encoder not running")
	}
	e.closeMu.Unlock()

	_, err := e.stdin.Write(samples)
	return err
}

// ReadFrame reads an encoded AAC frame (non-blocking)
func (e *AACEncoder) ReadFrame() []byte {
	select {
	case frame := <-e.outputChan:
		return frame
	default:
		return nil
	}
}

// Close stops the encoder
func (e *AACEncoder) Close() error {
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
func (e *AACEncoder) GetStderr() string {
	return e.stderr.String()
}
