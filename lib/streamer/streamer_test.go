package streamer

import (
	"context"
	"encoding/hex"
	"testing"
	"time"
)

func newTestConfig() *Config {
	return &Config{
		GRPCAddress:     "localhost:9090",
		PopSignerAPIKey: "test-key",
		PopSignerKeyID:  "test-id",
		ChainID:         "mocha-4",
		Timeout:         60 * time.Second,
	}
}

func TestNewStreamer(t *testing.T) {
	cfg := newTestConfig()

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
	if streamer.namespace.IsEmpty() {
		t.Error("expected non-empty namespace")
	}
}

func TestNewStreamer_GeneratesUniqueNamespaces(t *testing.T) {
	cfg := newTestConfig()

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
	cfg := newTestConfig()
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

	// Should match the raw namespace bytes
	expected := hex.EncodeToString(streamer.namespace.Bytes())
	if nsHex != expected {
		t.Errorf("NamespaceHex mismatch: got %s, want %s", nsHex, expected)
	}
}

func TestStreamer_Close(t *testing.T) {
	cfg := newTestConfig()
	streamer, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	// Close should not panic when conn is nil
	err = streamer.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestStreamer_Run_NotConnected(t *testing.T) {
	cfg := newTestConfig()
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

func TestConfig_CustomValues(t *testing.T) {
	cfg := &Config{
		GRPCAddress:     "custom:1234",
		PopSignerAPIKey: "my-api-key",
		PopSignerKeyID:  "my-key-id",
		ChainID:         "celestia",
		Timeout:         120 * time.Second,
		GasPrice:        0.002,
	}

	if cfg.GRPCAddress != "custom:1234" {
		t.Errorf("GRPCAddress mismatch")
	}
	if cfg.PopSignerAPIKey != "my-api-key" {
		t.Errorf("PopSignerAPIKey mismatch")
	}
	if cfg.PopSignerKeyID != "my-key-id" {
		t.Errorf("PopSignerKeyID mismatch")
	}
	if cfg.ChainID != "celestia" {
		t.Errorf("ChainID mismatch")
	}
	if cfg.Timeout != 120*time.Second {
		t.Errorf("Timeout mismatch")
	}
	if cfg.GasPrice != 0.002 {
		t.Errorf("GasPrice mismatch")
	}
}

func TestStreamer_Run_ContextCancelled(t *testing.T) {
	cfg := newTestConfig()
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
	cfg := newTestConfig()
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
	cfg := newTestConfig()
	streamer, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}

	// Namespace should not be empty
	if streamer.namespace.IsEmpty() {
		t.Error("namespace should not be empty")
	}

	// Verify namespace hex encoding works
	nsHex := streamer.NamespaceHex()
	if len(nsHex) == 0 {
		t.Error("namespace hex should not be empty")
	}
	nsBytes := streamer.namespace.Bytes()
	if len(nsHex) != len(nsBytes)*2 {
		t.Errorf("namespace hex length mismatch: %d vs %d", len(nsHex), len(nsBytes)*2)
	}
}

func TestCallCtx_UsesConfiguredTimeout(t *testing.T) {
	cfg := newTestConfig()
	cfg.Timeout = 50 * time.Millisecond
	s, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("NewStreamer: %v", err)
	}

	parent := context.Background()
	cctx, cancel := s.callCtx(parent)
	defer cancel()

	dl, ok := cctx.Deadline()
	if !ok {
		t.Fatal("callCtx should attach a deadline")
	}
	remaining := time.Until(dl)
	if remaining <= 0 || remaining > cfg.Timeout {
		t.Fatalf("deadline out of expected window: remaining=%v, timeout=%v", remaining, cfg.Timeout)
	}

	// Actually expires.
	select {
	case <-cctx.Done():
	case <-time.After(cfg.Timeout * 4):
		t.Fatal("callCtx did not expire within 4x the configured timeout")
	}
}

func TestCallCtx_FallsBackToDefault(t *testing.T) {
	cfg := newTestConfig()
	cfg.Timeout = 0 // unset → DefaultTimeout
	s, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("NewStreamer: %v", err)
	}

	cctx, cancel := s.callCtx(context.Background())
	defer cancel()

	dl, ok := cctx.Deadline()
	if !ok {
		t.Fatal("callCtx should attach a deadline even when Timeout is zero")
	}
	remaining := time.Until(dl)
	// Allow some slack but must be in the DefaultTimeout ballpark.
	if remaining <= 0 || remaining > DefaultTimeout {
		t.Fatalf("deadline not derived from DefaultTimeout: remaining=%v, default=%v", remaining, DefaultTimeout)
	}
	if DefaultTimeout-remaining > time.Second {
		t.Fatalf("deadline too far from DefaultTimeout: remaining=%v, default=%v", remaining, DefaultTimeout)
	}
}

func TestCallCtx_RespectsParentCancellation(t *testing.T) {
	cfg := newTestConfig()
	cfg.Timeout = time.Hour // long enough that parent cancel must dominate
	s, err := NewStreamer(cfg)
	if err != nil {
		t.Fatalf("NewStreamer: %v", err)
	}

	parent, parentCancel := context.WithCancel(context.Background())
	cctx, cancel := s.callCtx(parent)
	defer cancel()

	parentCancel()

	select {
	case <-cctx.Done():
	case <-time.After(time.Second):
		t.Fatal("callCtx should be cancelled when parent is cancelled")
	}
}

func TestStreamer_MultipleClose(t *testing.T) {
	cfg := newTestConfig()
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
