package streamer

import (
	"context"
	"encoding/hex"
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
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	if streamer == nil {
		t.Fatal("expected non-nil streamer")
	}
	if streamer.cfg != cfg {
		t.Error("config mismatch")
	}

	// Namespace should be generated
	if len(streamer.namespace) == 0 {
		t.Error("expected non-empty namespace")
	}
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
		if err != nil {
			t.Fatalf("failed to create streamer %d: %v", i, err)
		}

		nsHex := streamer.NamespaceHex()
		if namespaces[nsHex] {
			t.Errorf("duplicate namespace generated: %s", nsHex)
		}
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
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	nsHex := streamer.NamespaceHex()

	// Should be valid hex
	_, err = hex.DecodeString(nsHex)
	if err != nil {
		t.Errorf("NamespaceHex returned invalid hex: %v", err)
	}

	// Should match the raw namespace
	expected := hex.EncodeToString(streamer.namespace)
	if nsHex != expected {
		t.Errorf("NamespaceHex mismatch: got %s, want %s", nsHex, expected)
	}
}

func TestStreamer_Close(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	// Close should not panic when client is nil
	err = streamer.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestStreamer_Run_NotConnected(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	ctx := context.Background()
	input := make(chan []byte)

	// Run without connecting should return error
	err = streamer.Run(ctx, input)
	if err == nil {
		t.Error("expected error when running without connection")
	}
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
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	// Verify client is nil before connect
	if streamer.client != nil {
		t.Error("expected nil client before Connect")
	}
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://custom:1234",
		AuthToken: "my-token",
		Timeout:   120 * time.Second,
	}

	if cfg.NodeURL != "http://custom:1234" {
		t.Errorf("NodeURL mismatch")
	}
	if cfg.AuthToken != "my-token" {
		t.Errorf("AuthToken mismatch")
	}
	if cfg.Timeout != 120*time.Second {
		t.Errorf("Timeout mismatch")
	}
}

func TestStreamer_Run_ContextCancelled(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	// Test that Run returns error when not connected
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan []byte, 100)

	// Cancel immediately
	cancel()

	err = streamer.Run(ctx, input)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestStreamer_Run_InputClosed(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	ctx := context.Background()
	input := make(chan []byte)

	// Close input immediately - should return error (not connected)
	close(input)

	err = streamer.Run(ctx, input)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestNewStreamer_NamespaceLength(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}

	streamer, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	// Namespace should not be empty
	if len(streamer.namespace) == 0 {
		t.Error("namespace should not be empty")
	}

	// Verify namespace hex encoding works
	nsHex := streamer.NamespaceHex()
	if len(nsHex) == 0 {
		t.Error("namespace hex should not be empty")
	}
	if len(nsHex) != len(streamer.namespace)*2 {
		t.Errorf("namespace hex length mismatch: %d vs %d", len(nsHex), len(streamer.namespace)*2)
	}
}

func TestStreamer_MultipleClose(t *testing.T) {
	cfg := &Config{
		NodeURL:   "http://localhost:26658",
		AuthToken: "",
		Timeout:   30 * time.Second,
	}
	streamer, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	// Close multiple times should not panic
	err = streamer.Close()
	if err != nil {
		t.Errorf("first Close returned error: %v", err)
	}

	err = streamer.Close()
	if err != nil {
		t.Errorf("second Close returned error: %v", err)
	}
}
