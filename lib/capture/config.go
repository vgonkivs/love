package capture

// Config contains settings for video capturing
type Config struct {
	DeviceID          int    // Camera device ID (0, 1, 2, etc.)
	Width             int    // Capture width
	Height            int    // Capture height
	FPS               int    // Frames per second
	EnablePreview     bool   // Enable local preview window
	PreviewWindowName string // Name of the preview window
}
