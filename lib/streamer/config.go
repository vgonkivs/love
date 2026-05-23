package streamer

import "time"

// DefaultTimeout caps each individual gRPC call (BroadcastTx, QueryAccount).
// Picked to match the popsigner SDK's own HTTP client timeout so that both
// halves of submitBlob (remote signing + tx broadcast) have a similar bound.
// A whole submitBlob may still take up to ~2x this in the worst case
// (sign + broadcast + sequence-mismatch retry).
const DefaultTimeout = 30 * time.Second

// Config contains streamer settings
type Config struct {
	GRPCAddress     string        // Consensus node gRPC address (e.g. "localhost:9090")
	PopSignerAPIKey string        // POPSigner cloud API key
	PopSignerKeyID  string        // Key name or UUID in POPSigner
	ChainID         string        // Chain ID (e.g. "mocha-4", "celestia")
	Timeout         time.Duration // Per-call gRPC timeout; <= 0 falls back to DefaultTimeout.
	GasPrice        float64       // utia per gas unit (0 = use default min gas price)
}
