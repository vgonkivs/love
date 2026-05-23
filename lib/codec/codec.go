package codec

const (
	// ChunkSize is the target size for each blob (2MB = ~8 seconds of A/V at 2Mbps)
	ChunkSize = 1974272 // 2MB
)

// Codec identifiers carried in the entrypoint blob so the viewer can pick
// the matching decoder. Legacy JPEG entrypoints omit the byte entirely
// so the zero value must remain CodecIDJPEG; the viewer rejects anything
// that is not CodecIDTS as an unsupported wire format.
const (
	CodecIDJPEG byte = 0
	CodecIDH264 byte = 1
	CodecIDTS   byte = 2 // H.264 video + AAC audio in MPEG-TS
)

// EntrypointMarker tags the small metadata blob that opens every stream
// (codec ID, dimensions, sample rate, channels, fps). StreamEndMarker
// tags the optional end-of-stream blob.
var (
	EntrypointMarker = []byte{'E', 'N', 'T', 'R'}
	StreamEndMarker  = []byte{'E', 'N', 'D', 'S'}
)
