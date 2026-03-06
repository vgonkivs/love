package streamer

import "time"

// Config contains streamer settings
type Config struct {
	GRPCAddress     string        // Consensus node gRPC address (e.g. "localhost:9090")
	PopSignerAPIKey string        // POPSigner cloud API key
	PopSignerKeyID  string        // Key name or UUID in POPSigner
	ChainID         string        // Chain ID (e.g. "mocha-4", "celestia")
	Timeout         time.Duration // Timeout for blob submission
	GasPrice        float64       // utia per gas unit (0 = use default min gas price)
}
