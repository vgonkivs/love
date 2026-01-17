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
func (s *Streamer) Run(ctx context.Context, input <-chan []byte) error {
	if s.client == nil {
		return fmt.Errorf("streamer not connected, call Connect() first")
	}

	log.Printf("Streamer: running (namespace: %s)", s.NamespaceHex())

	var blobCount uint64
	for {
		select {
		case <-ctx.Done():
			log.Printf("Streamer: stopping (submitted %d blobs)", blobCount)
			return nil
		case data, ok := <-input:
			if !ok {
				log.Printf("Streamer: input closed (submitted %d blobs)", blobCount)
				return nil
			}

			// Create blob from data
			b, err := blob.NewBlobV0(s.namespace, data)
			if err != nil {
				log.Printf("Streamer: failed to create blob: %v", err)
				continue
			}

			// Submit blob to Celestia with default options
			height, err := s.client.Blob.Submit(ctx, []*blob.Blob{b}, blob.NewSubmitOptions())
			if err != nil {
				log.Printf("Streamer: failed to submit blob: %v", err)
				continue
			}

			blobCount++
			log.Printf("Streamer: submitted blob %d (%d bytes) at height %d", blobCount, len(data), height)
		}
	}
}


