package codec

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// AACDecoder decodes AAC audio to raw PCM using ffmpeg
type AACDecoder struct {
	sampleRate int
	channels   int

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

// AACDecoderConfig holds configuration for the AAC decoder
type AACDecoderConfig struct {
	SampleRate int
	Channels   int
}

// NewAACDecoder creates a new AAC decoder
func NewAACDecoder(cfg AACDecoderConfig) *AACDecoder {
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 44100
	}
	if cfg.Channels == 0 {
		cfg.Channels = 1
	}

	return &AACDecoder{
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		outputChan: make(chan []byte, 100),
		done:       make(chan struct{}),
	}
}

// Start initializes the ffmpeg process for AAC decoding
func (d *AACDecoder) Start() error {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	if d.started {
		return fmt.Errorf("decoder already started")
	}

	// Build ffmpeg command for AAC decoding
	// Input: AAC ADTS from pipe
	// Output: raw S16LE PCM to pipe
	d.cmd = exec.Command("ffmpeg",
		"-f", "aac",
		"-i", "pipe:0",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", d.sampleRate),
		"-ac", fmt.Sprintf("%d", d.channels),
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

// readOutput continuously reads decoded PCM data from ffmpeg
func (d *AACDecoder) readOutput() {
	defer close(d.outputChan)

	// Read ~20ms of audio at a time
	bufSize := d.sampleRate / 50 * d.channels * 2
	buf := make([]byte, bufSize)

	for {
		select {
		case <-d.done:
			return
		default:
		}

		n, err := d.stdout.Read(buf)
		if err != nil {
			return
		}

		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			select {
			case d.outputChan <- data:
			case <-d.done:
				return
			}
		}
	}
}

// Write writes AAC data to the decoder
func (d *AACDecoder) Write(aacData []byte) error {
	d.closeMu.Lock()
	if d.closed || !d.started {
		d.closeMu.Unlock()
		return fmt.Errorf("decoder not running")
	}
	d.closeMu.Unlock()

	_, err := d.stdin.Write(aacData)
	return err
}

// ReadSamples reads decoded PCM samples (non-blocking)
func (d *AACDecoder) ReadSamples() []byte {
	select {
	case samples := <-d.outputChan:
		return samples
	default:
		return nil
	}
}

// Close stops the decoder
func (d *AACDecoder) Close() error {
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
func (d *AACDecoder) GetStderr() string {
	return d.stderr.String()
}
