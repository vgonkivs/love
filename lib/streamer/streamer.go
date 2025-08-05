package streamer

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	client "github.com/celestiaorg/celestia-openrpc"
	"github.com/celestiaorg/celestia-openrpc/types/blob"
	"github.com/celestiaorg/celestia-openrpc/types/share"
)

// Entrypoint blob marker
var EntrypointMarker = []byte{'E', 'N', 'T', 'R'}

// Streamer posts blobs to Celestia blockchain
type Streamer struct {
	cfg       *Config
	client    *client.Client
	namespace share.Namespace
}

// NewStreamer creates a new streamer with a random namespace
func NewStreamer(cfg *Config) (*Streamer, error) {
	// Generate random namespace bytes (10 bytes for user-defined namespace)
	nsBytes := make([]byte, 10)
	_, err := rand.Read(nsBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate namespace: %w", err)
	}

	// Create namespace with version 0 prefix
	namespace, err := share.NewBlobNamespaceV0(nsBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace: %w", err)
	}

	return &Streamer{
		cfg:       cfg,
		namespace: namespace,
	}, nil
}

// Connect establishes connection to Celestia node
func (s *Streamer) Connect(ctx context.Context) error {
	c, err := client.NewClient(ctx, s.cfg.NodeURL, s.cfg.AuthToken)
	if err != nil {
		return fmt.Errorf("failed to connect to Celestia node: %w", err)
	}
	s.client = c
	log.Printf("Streamer: connected to Celestia node at %s", s.cfg.NodeURL)
	return nil
}

// Close closes the connection to Celestia node
func (s *Streamer) Close() error {
	if s.client != nil {
		s.client.Close()
	}
	return nil
}

// NamespaceHex returns the namespace as hex string
func (s *Streamer) NamespaceHex() string {
	return hex.EncodeToString(s.namespace)
}

// Run receives blobs from input channel and submits them to Celestia
// Outputs stream info (namespace + start height) to console on first blob
// Batches all available blobs together in a single submission
func (s *Streamer) Run(ctx context.Context, input <-chan []byte) error {
	if s.client == nil {
		return fmt.Errorf("not connected to Celestia node")
	}

	chunkNum := 0
	var startHeight uint64

	log.Printf("Streamer: starting blob submission, namespace: %s", s.NamespaceHex())

	for {
		select {
		case <-ctx.Done():
			log.Printf("Streamer: stopping, submitted %d blobs", chunkNum)
			return nil

		case blobData, ok := <-input:
			if !ok {
				log.Printf("Streamer: input closed, submitted %d blobs", chunkNum)
				return nil
			}

			// Collect all available blobs from channel
			blobsData := [][]byte{blobData}
		drainLoop:
			for {
				select {
				case data, ok := <-input:
					if !ok {
						break drainLoop
					}
					blobsData = append(blobsData, data)
				default:
					break drainLoop
				}
			}

			// Create blobs
			blobs := make([]*blob.Blob, 0, len(blobsData))
			for _, data := range blobsData {
				b, err := blob.NewBlobV0(s.namespace, data)
				if err != nil {
					log.Printf("Streamer: failed to create blob %d: %v", chunkNum+len(blobs), err)
					continue
				}
				blobs = append(blobs, b)
			}

			if len(blobs) == 0 {
				continue
			}

			// Submit batch to Celestia with default options
			height, err := s.client.Blob.Submit(ctx, blobs, blob.NewSubmitOptions())
			if err != nil {
				log.Printf("Streamer: failed to submit %d blobs: %v", len(blobs), err)
				continue
			}

			// First blob - output stream info
			if chunkNum == 0 {
				startHeight = height
				fmt.Println()
				fmt.Println("=== STREAM STARTED ===")
				fmt.Printf("Namespace: %s\n", s.NamespaceHex())
				fmt.Printf("Start Height: %d\n", startHeight)
				fmt.Println("======================")
				fmt.Println()
			}

			totalBytes := 0
			for _, data := range blobsData {
				totalBytes += len(data)
			}
			log.Printf("Streamer: %d blobs posted at height %d (%d bytes total)", len(blobs), height, totalBytes)
			chunkNum += len(blobs)
		}
	}
}

// SubmitBlob submits a single blob to Celestia
func (s *Streamer) SubmitBlob(ctx context.Context, data []byte) (uint64, error) {
	if s.client == nil {
		return 0, fmt.Errorf("not connected to Celestia node")
	}

	b, err := blob.NewBlobV0(s.namespace, data)
	if err != nil {
		return 0, fmt.Errorf("failed to create blob: %w", err)
	}

	height, err := s.client.Blob.Submit(ctx, []*blob.Blob{b}, blob.NewSubmitOptions())
	if err != nil {
		return 0, fmt.Errorf("failed to submit blob: %w", err)
	}

	return height, nil
}

// CreateEntrypointBlob creates an entrypoint blob with stream metadata
// Format: ENTR (4 bytes) + sample_rate (4 bytes) + channels (1 byte) + fps (1 byte)
func CreateEntrypointBlob(sampleRate int, channels int, fps int) []byte {
	data := make([]byte, 10)
	copy(data[:4], EntrypointMarker)
	binary.LittleEndian.PutUint32(data[4:8], uint32(sampleRate))
	data[8] = byte(channels)
	data[9] = byte(fps)
	return data
}

// ParseEntrypointBlob parses an entrypoint blob
// Returns sampleRate, channels, fps, and whether it's a valid entrypoint
func ParseEntrypointBlob(data []byte) (sampleRate int, channels int, fps int, valid bool) {
	if len(data) < 10 {
		return 0, 0, 0, false
	}
	if data[0] != 'E' || data[1] != 'N' || data[2] != 'T' || data[3] != 'R' {
		return 0, 0, 0, false
	}
	sampleRate = int(binary.LittleEndian.Uint32(data[4:8]))
	channels = int(data[8])
	fps = int(data[9])
	return sampleRate, channels, fps, true
}

// SequencedBlob wraps blob data with a sequence number for ordering
type SequencedBlob struct {
	Sequence uint64
	Data     []byte
}

// RunAsync receives blobs from input channel and submits them asynchronously
// First submits an entrypoint blob to get the starting height, then submits
// subsequent blobs without waiting for confirmation.
// Blobs are assigned sequence numbers for ordering on playback.
func (s *Streamer) RunAsync(ctx context.Context, input <-chan []byte, sampleRate, channels, fps int) error {
	if s.client == nil {
		return fmt.Errorf("not connected to Celestia node")
	}

	log.Printf("Streamer: starting async blob submission, namespace: %s", s.NamespaceHex())

	// Submit entrypoint blob first to get starting height (this one is synchronous)
	entrypointData := CreateEntrypointBlob(sampleRate, channels, fps)
	startHeight, err := s.SubmitBlob(ctx, entrypointData)
	if err != nil {
		return fmt.Errorf("failed to submit entrypoint blob: %w", err)
	}

	fmt.Println()
	fmt.Println("=== STREAM STARTED ===")
	fmt.Printf("Namespace: %s\n", s.NamespaceHex())
	fmt.Printf("Start Height: %d\n", startHeight)
	fmt.Println("======================")
	fmt.Println()

	// Track submission stats
	var submittedCount atomic.Uint64
	var failedCount atomic.Uint64
	var sequence atomic.Uint64

	// Worker pool for async submissions
	const numWorkers = 3
	workChan := make(chan SequencedBlob, 10)
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case sb, ok := <-workChan:
					if !ok {
						return
					}
					// Prepend sequence number to blob data
					seqData := make([]byte, 8+len(sb.Data))
					binary.LittleEndian.PutUint64(seqData[:8], sb.Sequence)
					copy(seqData[8:], sb.Data)

					b, err := blob.NewBlobV0(s.namespace, seqData)
					if err != nil {
						log.Printf("Streamer[%d]: failed to create blob seq=%d: %v", workerID, sb.Sequence, err)
						failedCount.Add(1)
						continue
					}

					_, err = s.client.Blob.Submit(ctx, []*blob.Blob{b}, blob.NewSubmitOptions())
					if err != nil {
						log.Printf("Streamer[%d]: failed to submit blob seq=%d: %v", workerID, sb.Sequence, err)
						failedCount.Add(1)
						continue
					}

					submittedCount.Add(1)
					if submittedCount.Load()%10 == 0 {
						log.Printf("Streamer: submitted %d blobs (failed: %d)", submittedCount.Load(), failedCount.Load())
					}
				}
			}
		}(i)
	}

	// Main loop: receive blobs and dispatch to workers
	for {
		select {
		case <-ctx.Done():
			close(workChan)
			wg.Wait()
			log.Printf("Streamer: stopping, submitted %d blobs (failed: %d)", submittedCount.Load(), failedCount.Load())
			return nil

		case blobData, ok := <-input:
			if !ok {
				close(workChan)
				wg.Wait()
				log.Printf("Streamer: input closed, submitted %d blobs (failed: %d)", submittedCount.Load(), failedCount.Load())
				return nil
			}

			seq := sequence.Add(1)
			select {
			case workChan <- SequencedBlob{Sequence: seq, Data: blobData}:
			case <-ctx.Done():
				close(workChan)
				wg.Wait()
				return nil
			}
		}
	}
}
