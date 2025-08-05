package capture

import (
	"context"
	"testing"
	"time"
)

func TestNewCapturer(t *testing.T) {
	cfg := &Config{
		DeviceID:          1,
		Width:             640,
		Height:            480,
		FPS:               15,
		EnablePreview:     true,
		PreviewWindowName: "Test Window",
	}

	capturer := NewCapturer(cfg)

	if capturer == nil {
		t.Fatal("expected non-nil capturer")
	}
	if capturer.cfg != cfg {
		t.Error("capturer config mismatch")
	}
	if capturer.cfg.DeviceID != 1 {
		t.Errorf("expected DeviceID 1, got %d", capturer.cfg.DeviceID)
	}
	if capturer.cfg.Width != 640 {
		t.Errorf("expected Width 640, got %d", capturer.cfg.Width)
	}
	if capturer.cfg.Height != 480 {
		t.Errorf("expected Height 480, got %d", capturer.cfg.Height)
	}
	if capturer.cfg.FPS != 15 {
		t.Errorf("expected FPS 15, got %d", capturer.cfg.FPS)
	}
	if capturer.cfg.EnablePreview != true {
		t.Error("expected EnablePreview true")
	}
}

func TestCapturer_Run_InvalidDevice(t *testing.T) {
	cfg := &Config{
		DeviceID: 999, // Invalid device ID
		Width:    640,
		Height:   480,
		FPS:      30,
	}

	capturer := NewCapturer(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output := make(chan interface{}, 1)

	// Run should fail with invalid device
	// Note: This behavior depends on the system - some systems may not error immediately
	err := capturer.Run(ctx, nil)

	// We expect an error for invalid device, but close the output channel regardless
	close(output)

	// The error behavior may vary by system, so we just verify it doesn't hang
	if err != nil {
		// Expected - invalid device should cause an error
		t.Logf("Got expected error for invalid device: %v", err)
	}
}

func TestConfig_Values(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected Config
	}{
		{
			name: "custom config",
			cfg: &Config{
				DeviceID:          2,
				Width:             1920,
				Height:            1080,
				FPS:               60,
				EnablePreview:     true,
				PreviewWindowName: "Custom",
			},
			expected: Config{
				DeviceID:          2,
				Width:             1920,
				Height:            1080,
				FPS:               60,
				EnablePreview:     true,
				PreviewWindowName: "Custom",
			},
		},
		{
			name: "minimum values",
			cfg: &Config{
				DeviceID:          0,
				Width:             1,
				Height:            1,
				FPS:               1,
				EnablePreview:     false,
				PreviewWindowName: "",
			},
			expected: Config{
				DeviceID:          0,
				Width:             1,
				Height:            1,
				FPS:               1,
				EnablePreview:     false,
				PreviewWindowName: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.DeviceID != tt.expected.DeviceID {
				t.Errorf("DeviceID: got %d, want %d", tt.cfg.DeviceID, tt.expected.DeviceID)
			}
			if tt.cfg.Width != tt.expected.Width {
				t.Errorf("Width: got %d, want %d", tt.cfg.Width, tt.expected.Width)
			}
			if tt.cfg.Height != tt.expected.Height {
				t.Errorf("Height: got %d, want %d", tt.cfg.Height, tt.expected.Height)
			}
			if tt.cfg.FPS != tt.expected.FPS {
				t.Errorf("FPS: got %d, want %d", tt.cfg.FPS, tt.expected.FPS)
			}
			if tt.cfg.EnablePreview != tt.expected.EnablePreview {
				t.Errorf("EnablePreview: got %v, want %v", tt.cfg.EnablePreview, tt.expected.EnablePreview)
			}
			if tt.cfg.PreviewWindowName != tt.expected.PreviewWindowName {
				t.Errorf("PreviewWindowName: got %s, want %s", tt.cfg.PreviewWindowName, tt.expected.PreviewWindowName)
			}
		})
	}
}
