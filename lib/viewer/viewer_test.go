package viewer

import (
	"context"
	"encoding/hex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-openrpc/types/share"
)

func TestNewViewer(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		AuthToken:  "",
		BufferSize: 10,
		WindowName: "Celestia Live Stream",
		PollDelay:  500 * time.Millisecond,
	}

	// Create a valid namespace hex
	nsBytes := make([]byte, 10)
	for i := range nsBytes {
		nsBytes[i] = byte(i + 1)
	}
	namespace, err := share.NewBlobNamespaceV0(nsBytes)
	require.NoError(t, err)
	namespaceHex := hex.EncodeToString(namespace)

	viewer, err := NewViewer(cfg, namespaceHex, 12345, false)
	require.NoError(t, err)
	assert.NotNil(t, viewer)
}

func TestNewViewer_InvalidNamespaceHex(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	// Invalid hex string
	_, err := NewViewer(cfg, "not-valid-hex", 12345, false)
	require.Error(t, err)
}

func TestNewViewer_InvalidNamespace(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	// Valid hex but invalid namespace (wrong length)
	_, err := NewViewer(cfg, "0102", 12345, false)
	require.Error(t, err)
}

func TestViewer_Close(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	nsBytes := make([]byte, 10)
	namespace, _ := share.NewBlobNamespaceV0(nsBytes)
	namespaceHex := hex.EncodeToString(namespace)

	viewer, err := NewViewer(cfg, namespaceHex, 12345, false)
	require.NoError(t, err)

	// Close should not panic when client is nil
	err = viewer.Close()
	require.NoError(t, err)
}

func TestViewer_Run_NotConnected(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	nsBytes := make([]byte, 10)
	namespace, _ := share.NewBlobNamespaceV0(nsBytes)
	namespaceHex := hex.EncodeToString(namespace)

	viewer, err := NewViewer(cfg, namespaceHex, 12345, false)
	require.NoError(t, err)

	ctx := context.Background()

	// Run without connecting should return error
	err = viewer.Run(ctx)
	require.Error(t, err)
}

func TestViewer_ClientNilBeforeConnect(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	nsBytes := make([]byte, 10)
	namespace, _ := share.NewBlobNamespaceV0(nsBytes)
	namespaceHex := hex.EncodeToString(namespace)

	viewer, err := NewViewer(cfg, namespaceHex, 12345, false)
	require.NoError(t, err)
	assert.Nil(t, viewer.client)
}
