# LOVE

**L**ive **O**nchain **V**ideo **E**nvironment

---

## What is LOVE?

LOVE is a decentralized live streaming platform built on Celestia's data availability layer. It captures video and audio from your webcam/microphone, encodes them using H.264 video compression, multiplexes them into 2MB data chunks (~8 seconds of A/V), and submits them as blobs to the Celestia blockchain. Viewers can then fetch these blobs and play back the stream in real-time with synchronized audio and video.

## Features

- **Live Streaming**: Real-time video capture from webcam with configurable resolution and framerate
- **H.264 Video Compression**: Efficient video encoding using ffmpeg for optimal streaming
- **Audio Support**: Synchronized audio capture from microphone (16-bit PCM, configurable sample rate)
- **Local Preview**: Optional local preview window for monitoring your stream
- **On-chain Storage**: Stream data is stored as Celestia blobs with automatic gas estimation
- **A/V Sync**: Timestamp-based synchronization ensures proper audio/video playback
- **Background Prefetching**: Viewer fetches blobs in background for smooth playback
- **Decentralized**: No central server - streams go directly to the blockchain
- **Censorship Resistant**: Once on-chain, streams cannot be removed
- **Pluggable Codec**: Interface-based design allows swapping encoding implementations

## Quick Start

### Prerequisites

1. **Go 1.21+**

2. **ffmpeg** (required for H.264 encoding/decoding)
   ```bash
   # macOS
   brew install ffmpeg

   # Ubuntu/Debian
   apt install ffmpeg
   ```

3. **OpenCV 4.x** with GoCV bindings
   ```bash
   # macOS
   brew install opencv

   # Ubuntu/Debian
   apt install libopencv-dev
   ```

4. **Audio libraries** (Linux only)
   ```bash
   # Ubuntu/Debian (ALSA)
   apt install libasound2-dev
   ```

5. **Celestia light node** running locally (or remote node access)

6. **Auth token** for Celestia node

### Installation

```bash
git clone https://github.com/vgonkivs/love.git
cd love
make build
```

### Get Celestia Auth Token

```bash
celestia light auth admin --p2p.network <network>
```

## Usage

### Using Make (Recommended)

```bash
# Stream with local preview
make stream token=<auth_token>

# Stream with custom settings
make stream token=<auth_token> fps=15 width=640 height=480

# View a stream (historical playback)
make view token=<auth_token> namespace=<hex> start_height=<height>

# View a stream (live mode)
make view token=<auth_token> namespace=<hex> start_height=<height> live=true

# Show help
make help
```

### Using Binary Directly

```bash
# Basic streaming
./love stream -token <auth_token>

# Custom settings
./love stream -width 1920 -height 1080 -fps 30 -bitrate 4M -samplerate 48000 -token <auth_token>

# View a stream (historical playback)
./love view -namespace <namespace_hex> -height <start_height> -token <auth_token>

# View a stream (live mode - subscribe to new blobs)
./love view -namespace <namespace_hex> -height <start_height> -live -token <auth_token>
```

Press **ESC** to stop streaming or exit viewer.

### Make Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `token` | | Celestia auth token (required) |
| `node` | http://localhost:26658 | Celestia node URL |
| `camera` | 0 | Camera device ID |
| `width` | 1280 | Video width (pixels) |
| `height` | 720 | Video height (pixels) |
| `fps` | 30 | Frames per second |
| `namespace` | | Stream namespace hex (required for view) |
| `start_height` | | Start block height (required for view) |

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              STREAMING                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌──────────────────────────────────────────────────────────────────────┐  │
│   │                         Capturer                                      │  │
│   │  1. Send entrypoint blob (metadata)                                  │  │
│   │  2. Initialize devices:                                              │  │
│   │  ┌──────────┐                                                        │  │
│   │  │  Webcam  │──┐                                                     │  │
│   │  └──────────┘  │    ┌──────────────┐    ┌─────────────┐              │  │
│   │                ├───▶│   Encoder    │───▶│ Preview     │              │  │
│   │  ┌──────────┐  │    │  (H.264)     │    │ Window      │              │  │
│   │  │   Mic    │──┘    └──────────────┘    └─────────────┘              │  │
│   │  └──────────┘              │                                         │  │
│   │                            ▼                                         │  │
│   │                      2MB Blobs                                       │  │
│   │                      3. Send stream end blob                         │  │
│   └──────────────────────────────────────────────────────────────────────┘  │
│                                          │                                   │
│                                          ▼                                   │
│                              ┌──────────────────────┐                        │
│                              │      Streamer        │                        │
│                              │  - Random namespace  │                        │
│                              │  - Submit to Celestia│                        │
│                              └──────────┬───────────┘                        │
│                                         │                                    │
└─────────────────────────────────────────┼────────────────────────────────────┘
                                          │
                                          ▼
                               ┌──────────────────────┐
                               │   Celestia Network   │
                               │                      │
                               │   Blobs stored in    │
                               │   namespace at       │
                               │   sequential heights │
                               └──────────┬───────────┘
                                          │
┌─────────────────────────────────────────┼────────────────────────────────────┐
│                              VIEWING    │                                    │
├─────────────────────────────────────────┼────────────────────────────────────┤
│                                         ▼                                    │
│   ┌──────────────────────────────────────────────────────────────────────┐  │
│   │                          Viewer                                       │  │
│   │                                                                       │  │
│   │  ┌─────────────────┐    ┌──────────────┐    ┌────────────────────┐   │  │
│   │  │ Background      │───▶│   Decoder    │───▶│  Display (GoCV)    │   │  │
│   │  │ Blob Fetcher    │    │  (H.264)     │    └────────────────────┘   │  │
│   │  │ (prefetching)   │    │              │    ┌────────────────────┐   │  │
│   │  └─────────────────┘    │              │───▶│  Audio Player      │   │  │
│   │                         └──────────────┘    │  (malgo)           │   │  │
│   │                                             └────────────────────┘   │  │
│   │  A/V Sync: Video paced by timestamps, audio plays at sample rate    │  │
│   └──────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Codec Interface

LOVE uses a pluggable codec architecture. The current implementation is `H264Encoder/Decoder` using ffmpeg, but the interface allows for alternative implementations:

```go
// Encoder encodes video and audio frames for streaming
type Encoder interface {
    EncodeVideo(frame gocv.Mat, timestamp time.Duration, sequence uint32) ([]byte, error)
    EncodeAudio(samples []byte, timestamp time.Duration, sequence uint32) ([]byte, error)
    CreateEntrypoint(sampleRate int, channels int) []byte
    CreateStreamEnd(totalDuration time.Duration, totalFrames uint32) []byte
}

// Decoder decodes multiplexed video and audio frames
type Decoder interface {
    Decode(data []byte) (*DecodedFrame, int)
    ParseEntrypoint(data []byte) (sampleRate int, channels int, err error)
}
```

## Data Format

### Frame Header (20 bytes)

Each video/audio frame is prefixed with a header:

```
┌───────────┬───────────┬─────────────────┬──────────────┐
│  Marker   │   Size    │   Timestamp     │   Sequence   │
│  4 bytes  │  4 bytes  │    8 bytes      │   4 bytes    │
├───────────┼───────────┼─────────────────┼──────────────┤
│ "H264" or │  Payload  │  Nanoseconds    │   Frame      │
│ "AUDF"    │  length   │  since start    │   number     │
└───────────┴───────────┴─────────────────┴──────────────┘
```

- **H264**: H.264 encoded video frame (may contain multiple NAL units: SPS, PPS, IDR, P-frames)
- **AUDF**: Audio frame (16-bit PCM samples)

### Blob Structure

Frames are accumulated into 2MB blobs (~8 seconds of A/V at 2Mbps video + 128kbps audio):

```
┌─────────────────────────────────────────────────────────┐
│                    2MB Blob                             │
├─────────────────────────────────────────────────────────┤
│ [Header][H.264 Data][Header][PCM Data][Header][H.264]...│
└─────────────────────────────────────────────────────────┘
```

### Entrypoint Blob

The Capturer sends an entrypoint blob first (before camera initialization) with stream metadata:

```
┌───────────┬─────────────┬──────────┬───────┬───────┬────────┐
│  Marker   │ Sample Rate │ Channels │ Codec │ Width │ Height │
│  4 bytes  │   4 bytes   │  1 byte  │1 byte │2 bytes│2 bytes │
├───────────┼─────────────┼──────────┼───────┼───────┼────────┤
│  "ENTR"   │   44100     │    1     │   1   │ 1280  │  720   │
└───────────┴─────────────┴──────────┴───────┴───────┴────────┘
```

Codec: 0 = JPEG (legacy), 1 = H.264

### Stream End Blob

When the stream ends gracefully (ESC or Ctrl+C), the Capturer sends a stream end notification:

```
┌───────────┬─────────────────────┬──────────────┐
│  Marker   │  Total Duration     │ Total Frames │
│  4 bytes  │     8 bytes         │   4 bytes    │
├───────────┼─────────────────────┼──────────────┤
│  "ENDS"   │  Nanoseconds        │   Count      │
└───────────┴─────────────────────┴──────────────┘
```

This allows viewers to distinguish between "stream ended gracefully" vs "stream stopped unexpectedly".

## Command Reference

### Stream Options

| Option | Default | Description |
|--------|---------|-------------|
| `-camera` | 0 | Camera device ID |
| `-width` | 1280 | Video width (pixels) |
| `-height` | 720 | Video height (pixels) |
| `-fps` | 30 | Frames per second |
| `-bitrate` | 2M | H.264 bitrate (e.g., 2M, 4M) |
| `-samplerate` | 44100 | Audio sample rate (Hz) |
| `-node` | http://localhost:26658 | Celestia node URL |
| `-token` | | Auth token (required) |

### View Options

| Option | Default | Description |
|--------|---------|-------------|
| `-namespace` | | Stream namespace hex (required) |
| `-height` | | Block height of entrypoint blob (required) |
| `-live` | false | Subscribe to live blobs (instead of historical playback) |
| `-node` | http://localhost:26658 | Celestia node URL |
| `-token` | | Auth token (required) |

## How It Works

### Streaming

1. **Entrypoint**: Capturer sends entrypoint blob with stream metadata (sample rate, channels, dimensions, codec)
2. **Initialize**: Capturer opens webcam (GoCV) and microphone (malgo)
3. **Preview**: Frames are displayed in local preview window (optional)
4. **Encode**: Video frames are H.264 encoded via ffmpeg (SPS/PPS/IDR combined), audio is 16-bit PCM
5. **Multiplex**: Frames are tagged with H264/AUDF markers and timestamps
6. **Chunk**: Data is accumulated into 2MB buffers inside Capturer (~8 seconds of A/V)
7. **Submit**: Blobs are submitted to Celestia via Streamer with automatic gas estimation
8. **Stream End**: When stopping gracefully, Capturer sends stream end blob with total duration and frame count

### Viewing

1. **Connect**: Viewer connects to Celestia node
2. **Find Entrypoint**: Locate the ENTR blob at specified height with stream parameters and codec type
3. **Create Decoder**: Initialize H.264 decoder based on codec identifier
4. **Background Fetch**:
   - **Historical mode** (default): Goroutine fetches blobs at sequential block heights
   - **Live mode** (`-live` flag): Goroutine subscribes to new blobs via `blob.Subscribe`
5. **Decode**: Parse frame headers, decode H.264 video via ffmpeg, extract PCM audio
6. **A/V Sync**: Video is paced by timestamps, audio plays at native sample rate through malgo
7. **Display**: Show video in window, play audio through speakers
8. **SPS/PPS Caching**: Decoder caches parameter sets for mid-stream joining

## Package Structure

```
love/
├── main.go              # CLI entry point
├── Makefile             # Build and run commands
├── cmd/
│   └── chaintest/       # H.264 encode/decode chain test app
│       └── main.go
├── lib/
│   ├── capture/         # Video + audio capture with embedded encoder
│   │   ├── capture.go   # Capturer implementation
│   │   └── config.go    # Capture configuration
│   ├── codec/           # Encoding/decoding interfaces and implementations
│   │   ├── interface.go # Encoder/Decoder interfaces
│   │   ├── jpeg.go      # JPEGCodec implementation (legacy)
│   │   ├── h264_encoder.go # H.264 encoder using ffmpeg
│   │   ├── h264_decoder.go # H.264 decoder using ffmpeg
│   │   ├── codec.go     # Shared constants and helpers
│   │   └── decoder.go   # Frame decoding utilities
│   ├── streamer/        # Celestia blob submission
│   │   ├── streamer.go  # Streamer implementation
│   │   └── config.go    # Streamer configuration
│   └── viewer/          # Blob fetching + playback with embedded decoder
│       ├── viewer.go    # Viewer implementation (background fetcher + A/V sync)
│       └── config.go    # Viewer configuration
```

## Troubleshooting

### Video not displaying
- Ensure ffmpeg is installed: `ffmpeg -version`
- Check if OpenCV/GoCV is properly installed

### Audio not working
- Linux: Install ALSA dev libraries: `apt install libasound2-dev`
- Check microphone permissions

### "non-existing PPS referenced" errors
- This happens when joining mid-stream before a keyframe
- Wait for the next keyframe or restart from an earlier height

### A/V out of sync
- Ensure you're using a fresh recording (old recordings may have sync issues)
- The viewer uses timestamp-based sync - video paced by timestamps, audio at native rate

## License

MIT

---

*Go live with LOVE*
