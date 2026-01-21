package viewer

import "time"

// Config contains viewer settings
type Config struct {
	NodeURL    string        // Celestia node RPC URL
	AuthToken  string        // Celestia node auth token
	BufferSize int           // Number of blobs to buffer
	WindowName string        // Display window title
	PollDelay  time.Duration // Delay between height polls
}
