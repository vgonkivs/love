package streamer

import "time"

// Config contains streamer settings
type Config struct {
	NodeURL   string        // Celestia node RPC URL
	AuthToken string        // Celestia node auth token
	Timeout   time.Duration // Timeout for blob submission
}
