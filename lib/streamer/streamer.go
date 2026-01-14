package streamer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"

	client "github.com/celestiaorg/celestia-openrpc"
	"github.com/celestiaorg/celestia-openrpc/types/blob"
	"github.com/celestiaorg/celestia-openrpc/types/share"
)

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


