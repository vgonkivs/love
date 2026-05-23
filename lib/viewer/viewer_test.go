package viewer

import (
	"context"
	"encoding/hex"
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
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}
	namespaceHex := hex.EncodeToString(namespace)

	viewer, err := NewViewer(cfg, namespaceHex, 12345)
	if err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	if viewer == nil {
		t.Fatal("expected non-nil viewer")
	}
	if viewer.height != 12345 {
		t.Errorf("expected height 12345, got %d", viewer.height)
	}
	if viewer.cfg != cfg {
		t.Error("config mismatch")
	}
}

func TestNewViewer_InvalidNamespaceHex(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	// Invalid hex string
	_, err := NewViewer(cfg, "not-valid-hex", 12345)
	if err == nil {
		t.Error("expected error for invalid namespace hex")
	}
}

func TestNewViewer_InvalidNamespace(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	// Valid hex but invalid namespace (wrong length)
	_, err := NewViewer(cfg, "0102", 12345)
	if err == nil {
		t.Error("expected error for invalid namespace")
	}
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

	viewer, err := NewViewer(cfg, namespaceHex, 12345)
	if err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	// Close should not panic when client is nil
	err = viewer.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
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

	viewer, err := NewViewer(cfg, namespaceHex, 12345)
	if err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	ctx := context.Background()

	// Run without connecting should return error
	err = viewer.Run(ctx)
	if err == nil {
		t.Error("expected error when running without connection")
	}
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

	viewer, err := NewViewer(cfg, namespaceHex, 12345)
	if err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	// Verify client is nil before connect
	if viewer.client != nil {
		t.Error("expected nil client before Connect")
	}
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://custom:1234",
		AuthToken:  "my-token",
		BufferSize: 20,
		WindowName: "Custom Window",
		PollDelay:  1 * time.Second,
	}

	if cfg.NodeURL != "http://custom:1234" {
		t.Errorf("NodeURL mismatch")
	}
	if cfg.AuthToken != "my-token" {
		t.Errorf("AuthToken mismatch")
	}
	if cfg.BufferSize != 20 {
		t.Errorf("BufferSize mismatch")
	}
	if cfg.WindowName != "Custom Window" {
		t.Errorf("WindowName mismatch")
	}
	if cfg.PollDelay != 1*time.Second {
		t.Errorf("PollDelay mismatch")
	}
}

func TestNewViewer_DifferentHeights(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	nsBytes := make([]byte, 10)
	namespace, _ := share.NewBlobNamespaceV0(nsBytes)
	namespaceHex := hex.EncodeToString(namespace)

	tests := []uint64{0, 1, 100, 1000000, 18446744073709551615}

	for _, height := range tests {
		viewer, err := NewViewer(cfg, namespaceHex, height)
		if err != nil {
			t.Fatalf("failed to create viewer with height %d: %v", height, err)
		}
		if viewer.height != height {
			t.Errorf("expected height %d, got %d", height, viewer.height)
		}
	}
}

func TestViewer_MultipleClose(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	nsBytes := make([]byte, 10)
	namespace, _ := share.NewBlobNamespaceV0(nsBytes)
	namespaceHex := hex.EncodeToString(namespace)

	viewer, err := NewViewer(cfg, namespaceHex, 12345)
	if err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	// Close multiple times should not panic
	err = viewer.Close()
	if err != nil {
		t.Errorf("first Close returned error: %v", err)
	}

	err = viewer.Close()
	if err != nil {
		t.Errorf("second Close returned error: %v", err)
	}
}

func TestNewViewer_NamespacePreserved(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	// Create specific namespace bytes
	nsBytes := make([]byte, 10)
	for i := range nsBytes {
		nsBytes[i] = byte(i * 17)
	}
	namespace, _ := share.NewBlobNamespaceV0(nsBytes)
	namespaceHex := hex.EncodeToString(namespace)

	viewer, err := NewViewer(cfg, namespaceHex, 12345)
	if err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	// Verify namespace is preserved
	if hex.EncodeToString(viewer.namespace.Bytes()) != namespaceHex {
		t.Error("namespace not preserved correctly")
	}
}

func TestNewViewer_EmptyNamespaceHex(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://localhost:26658",
		BufferSize: 10,
		PollDelay:  500 * time.Millisecond,
	}

	_, err := NewViewer(cfg, "", 12345)
	if err == nil {
		t.Error("expected error for empty namespace hex")
	}
}

// TestPlayAudio_BoundedBuffer verifies that playAudio drops the OLDEST
// samples when audioBuffer exceeds audioMaxBytes — the quick-patch
// behavior that prevents audio from drifting arbitrarily far ahead of
// the (paced) video stream.
func TestPlayAudio_BoundedBuffer(t *testing.T) {
	v := &Viewer{audioMaxBytes: 100}

	// Three writes of 60 bytes each: total 180, cap 100.
	first := make([]byte, 60)
	for i := range first {
		first[i] = 0xAA
	}
	second := make([]byte, 60)
	for i := range second {
		second[i] = 0xBB
	}
	third := make([]byte, 60)
	for i := range third {
		third[i] = 0xCC
	}

	v.playAudio(first, 0)
	v.playAudio(second, 0)
	v.playAudio(third, 0)

	if got := len(v.audioBuffer); got != 100 {
		t.Fatalf("buffer size: got %d, want 100", got)
	}
	// Newest 100 bytes must be the tail of (first+second+third) = last 60
	// of third (all 0xCC) preceded by last 40 of second (all 0xBB).
	for i := 0; i < 40; i++ {
		if v.audioBuffer[i] != 0xBB {
			t.Fatalf("audioBuffer[%d]: got %x, want 0xBB (oldest 0xAA bytes should have been dropped)", i, v.audioBuffer[i])
		}
	}
	for i := 40; i < 100; i++ {
		if v.audioBuffer[i] != 0xCC {
			t.Fatalf("audioBuffer[%d]: got %x, want 0xCC", i, v.audioBuffer[i])
		}
	}
}

// TestPlayAudio_UnboundedWhenMaxZero verifies the cap is opt-in:
// audioMaxBytes == 0 means no enforcement (preserves the legacy path
// for tests / unit consumers that don't go through startAudioPlayer).
func TestPlayAudio_UnboundedWhenMaxZero(t *testing.T) {
	v := &Viewer{audioMaxBytes: 0}
	v.playAudio(make([]byte, 1000), 0)
	v.playAudio(make([]byte, 1000), 0)
	if got := len(v.audioBuffer); got != 2000 {
		t.Fatalf("with audioMaxBytes=0 buffer should accumulate; got %d", got)
	}
}

func TestViewer_ConfigPreserved(t *testing.T) {
	cfg := &Config{
		NodeURL:    "http://test:1234",
		AuthToken:  "test-token",
		BufferSize: 50,
		WindowName: "Test Window",
		PollDelay:  2 * time.Second,
	}

	nsBytes := make([]byte, 10)
	namespace, _ := share.NewBlobNamespaceV0(nsBytes)
	namespaceHex := hex.EncodeToString(namespace)

	viewer, err := NewViewer(cfg, namespaceHex, 12345)
	if err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	if viewer.cfg.NodeURL != "http://test:1234" {
		t.Error("NodeURL not preserved")
	}
	if viewer.cfg.AuthToken != "test-token" {
		t.Error("AuthToken not preserved")
	}
	if viewer.cfg.BufferSize != 50 {
		t.Error("BufferSize not preserved")
	}
	if viewer.cfg.WindowName != "Test Window" {
		t.Error("WindowName not preserved")
	}
	if viewer.cfg.PollDelay != 2*time.Second {
		t.Error("PollDelay not preserved")
	}
}
