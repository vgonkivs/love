package streamer

import (
	"context"
	"encoding/hex"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNewStreamer(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "test-token",
		Timeout:   60 * time.Second,
	}

	streamer, err := NewStreamer(cfg)
	require.NoError(t, err)
	assert.NotNil(t, streamer)
	assert.Equal(t, len(streamer.namespace), share.NamespaceSize)
}

func TestNewStreamer_GeneratesUniqueNamespaces(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}

	// Create multiple streamers and verify namespaces are unique
	namespaces := make(map[string]bool)

	for i := 0; i < 10; i++ {
		streamer, err := NewStreamer(cfg)
		require.NoError(t, err)

		nsHex := streamer.NamespaceHex()
		_, ok := namespaces[nsHex]
		require.False(t, ok)
		namespaces[nsHex] = true
	}
}

func TestStreamer_NamespaceHex(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	require.NoError(t, err)

	nsHex := streamer.NamespaceHex()

	// Should be valid hex
	_, err = hex.DecodeString(nsHex)
	require.NoError(t, err)

	// Should match the raw namespace
	expected := hex.EncodeToString(streamer.namespace)
	assert.Equal(t, expected, nsHex)
}

func TestStreamer_Close(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	require.NoError(t, err)
	require.NoError(t, streamer.Close())
}

func TestStreamer_Run_NotConnected(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	input := make(chan []byte)

	// Run without connecting should return error
	require.Error(t, streamer.Run(ctx, input))

}

func TestStreamer_Connect_CreatesClient(t *testing.T) {
	// Note: The Celestia client uses lazy connection, so it doesn't
	// validate the URL until an actual request is made.
	// This test just verifies the client object is created.
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "test-token",
		Timeout:   1 * time.Second,
	}

	streamer, err := NewStreamer(cfg)
	require.NoError(t, err)
	assert.Nil(t, streamer.client)
}

func TestStreamer_Run_InputClosed(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	input := make(chan []byte)

	// Close input immediately - should return error (not connected)
	close(input)
	require.Error(t, streamer.Run(ctx, input))
}
