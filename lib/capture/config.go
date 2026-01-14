package capture

// Config contains settings for video and audio capturing
type Config struct {
	// Video settings
	DeviceID          int    // Camera device ID (0, 1, 2, etc.)
	Width             int    // Capture width
	Height            int    // Capture height
	FPS               int    // Frames per second
	EnablePreview     bool   // Enable local preview window
	PreviewWindowName string // Name of the preview window

	// Audio settings
	AudioDeviceID int // Audio device ID (-1 for default)
	SampleRate    int // Sample rate in Hz (e.g., 44100, 48000)
	Channels      int // Number of channels (1=mono, 2=stereo)
	AudioBuffer   int // Audio buffer size in frames
}
