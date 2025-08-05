package codec

import (
	"context"
	"encoding/binary"
	"log"
	"time"

	"gocv.io/x/gocv"

	"github.com/vgonkivs/blob/lib/audio"
)

const (
	// ChunkSize is the target size for each blob (1MB)
	ChunkSize = 1048576 // 1MB exactly
)

// Frame type markers for multiplexing audio and video
var (
	VideoFrameMarker = []byte{'V', 'I', 'D', 'F'} // Video frame marker
	AudioFrameMarker = []byte{'A', 'U', 'D', 'F'} // Audio frame marker
)

const (
	// FrameHeaderSize is the size of the frame header in bytes
	// 4 (marker) + 4 (size) + 8 (timestamp) + 4 (sequence) = 20 bytes
	FrameHeaderSize = 20
)

// EncodeFrameHeaderWithTimestamp creates a frame header with timestamp and sequence
func EncodeFrameHeaderWithTimestamp(frameType []byte, dataSize int, timestamp uint64, sequence uint32) []byte {
	header := make([]byte, FrameHeaderSize)
	copy(header[:4], frameType)
	binary.LittleEndian.PutUint32(header[4:8], uint32(dataSize))
	binary.LittleEndian.PutUint64(header[8:16], timestamp)
	binary.LittleEndian.PutUint32(header[16:20], sequence)
	return header
}

// Config contains codec settings
type Config struct {
	JPEGQuality int // JPEG compression quality (1-100)
}

// Codec encodes frames to JPEG and chunks them into 1MB blobs
type Codec struct {
	cfg *Config
}

// NewCodec creates a new codec
func NewCodec(cfg *Config) *Codec {
	return &Codec{
		cfg: cfg,
	}
}

// Run processes frames from input channel, encodes to JPEG, and outputs 1MB blobs
// This method blocks until context is cancelled or input channel is closed
func (c *Codec) Run(ctx context.Context, input <-chan gocv.Mat, output chan<- []byte) error {
	buffer := make([]byte, 0, ChunkSize)
	frameCount := 0
	blobCount := 0

	log.Printf("Codec: starting with JPEG quality %d, chunk size %d bytes", c.cfg.JPEGQuality, ChunkSize)

	for {
		select {
		case <-ctx.Done():
			// Flush remaining buffer if any
			if len(buffer) > 0 {
				select {
				case output <- buffer:
					log.Printf("Codec: flushed final blob %d (%d bytes)", blobCount, len(buffer))
				default:
				}
			}
			log.Printf("Codec: stopping, processed %d frames into %d blobs", frameCount, blobCount+1)
			return nil

		case frame, ok := <-input:
			if !ok {
				// Input channel closed, flush remaining buffer
				if len(buffer) > 0 {
					select {
					case output <- buffer:
						log.Printf("Codec: flushed final blob %d (%d bytes)", blobCount, len(buffer))
					case <-ctx.Done():
					}
				}
				log.Printf("Codec: input closed, processed %d frames into %d blobs", frameCount, blobCount+1)
				return nil
			}

			// Encode frame to JPEG
			buf, err := gocv.IMEncodeWithParams(".jpg", frame, []int{gocv.IMWriteJpegQuality, c.cfg.JPEGQuality})
			if err != nil {
				frame.Close()
				log.Printf("Codec: failed to encode frame: %v", err)
				continue
			}

			frameData := buf.GetBytes()
			buf.Close()
			frame.Close()
			frameCount++

			// Add frame data to buffer
			buffer = append(buffer, frameData...)

			// Emit full 1MB chunks
			for len(buffer) >= ChunkSize {
				chunk := make([]byte, ChunkSize)
				copy(chunk, buffer[:ChunkSize])

				select {
				case output <- chunk:
					log.Printf("Codec: emitted blob %d (%d bytes)", blobCount, ChunkSize)
					blobCount++
				case <-ctx.Done():
					return nil
				}

				buffer = buffer[ChunkSize:]
			}
		}
	}
}

// RunWithAudio processes both video frames and audio samples, multiplexing them into 1MB blobs
// Frames are timestamped relative to stream start for A/V synchronization
func (c *Codec) RunWithAudio(ctx context.Context, videoInput <-chan gocv.Mat, audioInput <-chan audio.AudioData, output chan<- []byte) error {
	buffer := make([]byte, 0, ChunkSize)
	videoFrameCount := 0
	audioFrameCount := 0
	blobCount := 0
	var sequence uint32 = 0

	// Track stream start time for timestamps
	startTime := time.Now()

	log.Printf("Codec: starting with JPEG quality %d, chunk size %d bytes (audio+video, timestamped)", c.cfg.JPEGQuality, ChunkSize)

	emitChunks := func() {
		for len(buffer) >= ChunkSize {
			chunk := make([]byte, ChunkSize)
			copy(chunk, buffer[:ChunkSize])

			select {
			case output <- chunk:
				log.Printf("Codec: emitted blob %d (%d bytes)", blobCount, ChunkSize)
				blobCount++
			case <-ctx.Done():
				return
			}

			buffer = buffer[ChunkSize:]
		}
	}

	flushBuffer := func() {
		if len(buffer) > 0 {
			select {
			case output <- buffer:
				log.Printf("Codec: flushed final blob %d (%d bytes)", blobCount, len(buffer))
			case <-ctx.Done():
			default:
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			flushBuffer()
			log.Printf("Codec: stopping, processed %d video frames, %d audio chunks into %d blobs", videoFrameCount, audioFrameCount, blobCount+1)
			return nil

		case frame, ok := <-videoInput:
			if !ok {
				videoInput = nil
				if audioInput == nil {
					flushBuffer()
					log.Printf("Codec: all inputs closed, processed %d video frames, %d audio chunks into %d blobs", videoFrameCount, audioFrameCount, blobCount+1)
					return nil
				}
				continue
			}

			// Calculate timestamp
			timestamp := uint64(time.Since(startTime).Nanoseconds())

			// Encode frame to JPEG
			buf, err := gocv.IMEncodeWithParams(".jpg", frame, []int{gocv.IMWriteJpegQuality, c.cfg.JPEGQuality})
			if err != nil {
				frame.Close()
				log.Printf("Codec: failed to encode frame: %v", err)
				continue
			}

			frameData := buf.GetBytes()
			buf.Close()
			frame.Close()
			videoFrameCount++

			// Add frame header with timestamp and data to buffer
			header := EncodeFrameHeaderWithTimestamp(VideoFrameMarker, len(frameData), timestamp, sequence)
			sequence++
			buffer = append(buffer, header...)
			buffer = append(buffer, frameData...)

			emitChunks()

		case audioData, ok := <-audioInput:
			if !ok {
				audioInput = nil
				if videoInput == nil {
					flushBuffer()
					log.Printf("Codec: all inputs closed, processed %d video frames, %d audio chunks into %d blobs", videoFrameCount, audioFrameCount, blobCount+1)
					return nil
				}
				continue
			}

			// Calculate timestamp
			timestamp := uint64(time.Since(startTime).Nanoseconds())
			audioFrameCount++

			// Add audio header with timestamp and data to buffer
			header := EncodeFrameHeaderWithTimestamp(AudioFrameMarker, len(audioData.Samples), timestamp, sequence)
			sequence++
			buffer = append(buffer, header...)
			buffer = append(buffer, audioData.Samples...)

			emitChunks()
		}
	}
}
