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
	if hex.EncodeToString(viewer.namespace) != namespaceHex {
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
