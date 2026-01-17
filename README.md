# LOVE

**L**ive **O**nchain **V**ideo **E**nvironment

---

## What is LOVE?

LOVE is a decentralized live streaming platform built on Celestia's data availability layer. It captures video and audio from your webcam/microphone, encodes them using H.264 video compression, multiplexes them into 2MB data chunks (~8 seconds of A/V), and submits them as blobs to the Celestia blockchain. Viewers can then fetch these blobs and play back the stream in real-time with synchronized audio and video.

## Features

- **Live Streaming**: Real-time video capture from webcam with configurable resolution and framerate
- **H.264 Video Compression**: Efficient video encoding using ffmpeg for optimal streaming
- **Audio Support**: Synchronized audio capture from microphone (16-bit PCM, configurable sample rate)
- **Local Preview**: Always-on local preview window for monitoring your stream
- **On-chain Storage**: Stream data is stored as Celestia blobs with automatic gas estimation
- **A/V Sync**: Timestamps ensure proper audio/video synchronization
- **Decentralized**: No central server - streams go directly to the blockchain
- **Censorship Resistant**: Once on-chain, streams cannot be removed
- **Pluggable Codec**: Interface-based design allows swapping encoding implementations

## Quick Start

### Prerequisites

1. **Go 1.21+**
2. **OpenCV 4.x** with GoCV bindings (`brew install opencv` on macOS)
3. **ffmpeg** (required for H.264 encoding/decoding)
4. **Celestia light node** running locally (or remote node access) - optional for local mode
5. **Auth token** for Celestia node - optional for local mode

### Installation

```bash
git clone https://github.com/vgonkivs/love.git
cd love
go build -o love .
```

### Get Celestia Auth Token (if using Celestia)

```bash
celestia light auth admin --p2p.network <network>
```

## Usage

### Start Streaming

```bash
# Basic streaming (video + audio with local preview)
./love stream -token <auth_token>

# Custom settings
./love stream -width 1920 -height 1080 -fps 30 -bitrate 4M -samplerate 48000 -token <auth_token>

# Local mode (no Celestia token needed, preview only)
./love stream
```

The stream will open a local preview window automatically. Press **ESC** to stop streaming.

### View a Stream

```bash
./love view -namespace <namespace> -height <start_height> -token <auth_token>
```

Press **ESC** to exit.

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
│   │  │  Fetch Blobs    │───▶│   Decoder    │───▶│  Display (GoCV)    │   │  │
│   │  │  - A/V sync     │    │  (H.264)     │    └────────────────────┘   │  │
│   │  └─────────────────┘    │              │    ┌────────────────────┐   │  │
│   │                         │              │───▶│  Audio Player      │   │  │
│   │                         └──────────────┘    └────────────────────┘   │  │
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
    CreateEntrypoint(sampleRate int, channels int, fps int) []byte
    CreateStreamEnd(totalDuration time.Duration, totalFrames uint32) []byte
}

// Decoder decodes multiplexed video and audio frames
type Decoder interface {
    Decode(data []byte) (*DecodedFrame, int)
    ParseEntrypoint(data []byte) (sampleRate int, channels int, fps int, valid bool)
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

- **H264**: H.264 encoded video frame
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
┌───────────┬─────────────┬──────────┬─────────┬───────┬───────┬────────┐
│  Marker   │ Sample Rate │ Channels │   FPS   │ Codec │ Width │ Height │
│  4 bytes  │   4 bytes   │  1 byte  │ 1 byte  │1 byte │2 bytes│2 bytes │
├───────────┼─────────────┼──────────┼─────────┼───────┼───────┼────────┤
│  "ENTR"   │   44100     │    1     │   30    │   1   │ 1280  │  720   │
└───────────┴─────────────┴──────────┴─────────┴───────┴───────┴────────┘
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
| `-token` | | Auth token (optional for local mode) |

### View Options

| Option | Default | Description |
|--------|---------|-------------|
| `-namespace` | | Stream namespace (required) |
| `-height` | | Start block height (required) |
| `-node` | http://localhost:26658 | Celestia node URL |
| `-token` | | Auth token (required) |

## How It Works

### Streaming

1. **Entrypoint**: Capturer sends entrypoint blob with stream metadata (sample rate, channels, fps, dimensions, codec)
2. **Initialize**: Capturer opens webcam (GoCV) and microphone (malgo)
3. **Preview**: Frames are displayed in local preview window immediately after capture
4. **Encode**: Video frames are H.264 encoded via ffmpeg, audio is 16-bit PCM
5. **Multiplex**: Frames are tagged with H264/AUDF markers and timestamps
6. **Chunk**: Data is accumulated into 2MB buffers inside Capturer (~8 seconds of A/V)
7. **Submit**: Blobs are submitted to Celestia via Streamer with automatic gas estimation
8. **Stream End**: When stopping gracefully, Capturer sends stream end blob with total duration and frame count

### Viewing

1. **Viewer** connects to Celestia node
2. **Find Entrypoint**: Locate the ENTR blob with stream parameters and codec type
3. **Create Decoder**: Initialize H.264 or JPEG decoder based on codec identifier
4. **Fetch Blobs**: Poll for new blobs at sequential block heights
5. **Decode**: Parse frame headers, decode H.264/PCM data via the Decoder interface
6. **Sync**: Use timestamps to synchronize audio and video playback
7. **Display**: Show video in window, play audio through speakers

## Package Structure

```
love/
├── main.go              # CLI entry point
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
│       ├── viewer.go    # Viewer implementation
│       └── config.go    # Viewer configuration
```

## License

MIT

---

*Go live with LOVE*
