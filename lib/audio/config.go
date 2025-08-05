package audio

// Config contains settings for audio capturing
type Config struct {
	DeviceID   int // Audio device ID (-1 for default)
	SampleRate int // Sample rate in Hz (e.g., 44100, 48000)
	Channels   int // Number of channels (1=mono, 2=stereo)
	BufferSize int // Buffer size in frames
}
