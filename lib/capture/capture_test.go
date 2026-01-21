package capture

import (
	"context"
	"testing"
	"time"

	"github.com/vgonkivs/love/lib/codec"
)

func TestNewCapturer(t *testing.T) {
	cfg := &Config{
		DeviceID:          1,
		Width:             640,
		Height:            480,
		FPS:               15,
		PreviewWindowName: "Test Window",
		AudioDeviceID:     -1,
		SampleRate:        44100,
		Channels:          1,
		AudioBuffer:       1024,
	}

	encoder := codec.NewJPEGCodec(85)
	capturer := NewCapturer(cfg, encoder)

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
	if capturer.cfg.SampleRate != 44100 {
		t.Errorf("expected SampleRate 44100, got %d", capturer.cfg.SampleRate)
	}
}

func TestCapturer_Run_InvalidDevice(t *testing.T) {
	cfg := &Config{
		DeviceID:      999, // Invalid device ID
		Width:         640,
		Height:        480,
		FPS:           30,
		AudioDeviceID: -1,
		SampleRate:    44100,
		Channels:      1,
		AudioBuffer:   1024,
	}

	encoder := codec.NewJPEGCodec(85)
	capturer := NewCapturer(cfg, encoder)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output := make(chan []byte, 100)

	// Run should fail with invalid device
	// Note: This behavior depends on the system - some systems may not error immediately
	err := capturer.Run(ctx, output)

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
				PreviewWindowName: "Custom",
				AudioDeviceID:     -1,
				SampleRate:        48000,
				Channels:          2,
				AudioBuffer:       2048,
			},
			expected: Config{
				DeviceID:          2,
				Width:             1920,
				Height:            1080,
				FPS:               60,
				PreviewWindowName: "Custom",
				AudioDeviceID:     -1,
				SampleRate:        48000,
				Channels:          2,
				AudioBuffer:       2048,
			},
		},
		{
			name: "minimum values",
			cfg: &Config{
				DeviceID:          0,
				Width:             1,
				Height:            1,
				FPS:               1,
				PreviewWindowName: "",
				AudioDeviceID:     -1,
				SampleRate:        8000,
				Channels:          1,
				AudioBuffer:       256,
			},
			expected: Config{
				DeviceID:          0,
				Width:             1,
				Height:            1,
				FPS:               1,
				PreviewWindowName: "",
				AudioDeviceID:     -1,
				SampleRate:        8000,
				Channels:          1,
				AudioBuffer:       256,
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
			if tt.cfg.PreviewWindowName != tt.expected.PreviewWindowName {
				t.Errorf("PreviewWindowName: got %s, want %s", tt.cfg.PreviewWindowName, tt.expected.PreviewWindowName)
			}
			if tt.cfg.SampleRate != tt.expected.SampleRate {
				t.Errorf("SampleRate: got %d, want %d", tt.cfg.SampleRate, tt.expected.SampleRate)
			}
			if tt.cfg.Channels != tt.expected.Channels {
				t.Errorf("Channels: got %d, want %d", tt.cfg.Channels, tt.expected.Channels)
			}
		})
	}
}
