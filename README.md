# LOVE

**L**ive **O**nchain **V**ideo **E**nvironment

---

## What is LOVE?

LOVE is a decentralized live streaming platform built on Celestia's data availability layer. It captures video and audio from your webcam/microphone, encodes and multiplexes them into 1MB data chunks, and submits them as blobs to the Celestia blockchain. Viewers can then fetch these blobs and play back the stream in real-time with synchronized audio and video.

## Features

- **Live Streaming**: Real-time video capture from webcam with configurable resolution and framerate
- **Audio Support**: Synchronized audio capture from microphone (16-bit PCM, configurable sample rate)
- **On-chain Storage**: All stream data stored as Celestia blobs
- **A/V Sync**: Timestamps ensure proper audio/video synchronization
- **Decentralized**: No central server - streams go directly to the blockchain
- **Censorship Resistant**: Once on-chain, streams cannot be removed
- **Pluggable Codec**: Interface-based design allows swapping encoding implementations

## Quick Start

### Prerequisites

1. **Go 1.21+**
2. **OpenCV 4.x** with GoCV bindings (`brew install opencv` on macOS)
3. **Celestia light node** running locally (or remote node access)
4. **Auth token** for Celestia node

### Installation

```bash
git clone https://github.com/vgonkivs/love.git
cd love
go build -o love .
```

### Get Celestia Auth Token

```bash
celestia light auth admin --p2p.network <network>
```

## Usage

### Start Streaming

```bash
# Basic streaming (video + audio)
./love stream -token <auth_token>

# With local preview window
./love stream -preview -token <auth_token>

# Custom settings
./love stream -width 1920 -height 1080 -fps 30 -quality 90 -samplerate 48000 -token <auth_token>
```

When the stream starts, you'll see:

```
=== STREAM STARTED ===
Namespace: 00000000000000000000a1b2c3d4e5f6a7b8c9d0
Start Height: 1234567
======================
```

**Share the namespace and height with viewers!**

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
│   │  └──────────┘  │    ┌──────────────┐                                 │  │
│   │                ├───▶│   Encoder    │───▶ 1MB Blobs                   │  │
│   │  ┌──────────┐  │    │  (JPEGCodec) │                                 │  │
│   │  │   Mic    │──┘    └──────────────┘                                 │  │
│   │  └──────────┘                                                        │  │
│   └──────────────────────────────────────────────────────────────────────┘  │
│                                          │                                   │
│                                          ▼                                   │
│                              ┌──────────────────────┐                        │
│                              │      Streamer        │                        │
│                              │  - Random namespace  │                        │
│                              │  - Sync submission   │                        │
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
│   │  │  - A/V sync     │    │  (JPEGCodec) │    └────────────────────┘   │  │
│   │  └─────────────────┘    │              │    ┌────────────────────┐   │  │
│   │                         │              │───▶│  Audio Player      │   │  │
│   │                         └──────────────┘    └────────────────────┘   │  │
│   └──────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Codec Interface

LOVE uses a pluggable codec architecture. The current implementation is `JPEGCodec` (JPEG video + PCM audio), but the interface allows for alternative implementations:

```go
// Encoder encodes video and audio frames for streaming
type Encoder interface {
    EncodeVideo(frame gocv.Mat, timestamp time.Duration, sequence uint32) ([]byte, error)
    EncodeAudio(samples []byte, timestamp time.Duration, sequence uint32) ([]byte, error)
    CreateEntrypoint(sampleRate int, channels int, fps int) []byte
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
│ "VIDF" or │  Payload  │  Nanoseconds    │   Frame      │
│ "AUDF"    │  length   │  since start    │   number     │
└───────────┴───────────┴─────────────────┴──────────────┘
```

- **VIDF**: Video frame (JPEG encoded)
- **AUDF**: Audio frame (16-bit PCM samples)

### Blob Structure

Frames are accumulated into 1MB blobs:

```
┌─────────────────────────────────────────────────────────┐
│                    1MB Blob                             │
├─────────────────────────────────────────────────────────┤
│ [Header][JPEG Data][Header][PCM Data][Header][JPEG]...  │
└─────────────────────────────────────────────────────────┘
```

### Entrypoint Blob

The Capturer sends an entrypoint blob first (before camera initialization) with stream metadata:

```
┌───────────┬─────────────┬──────────┬─────────┐
│  Marker   │ Sample Rate │ Channels │   FPS   │
│  4 bytes  │   4 bytes   │  1 byte  │ 1 byte  │
├───────────┼─────────────┼──────────┼─────────┤
│  "ENTR"   │   44100     │    1     │   30    │
└───────────┴─────────────┴──────────┴─────────┘
```

## Command Reference

### Stream Options

| Option | Default | Description |
|--------|---------|-------------|
| `-camera` | 0 | Camera device ID |
| `-width` | 1280 | Video width (pixels) |
| `-height` | 720 | Video height (pixels) |
| `-fps` | 30 | Frames per second |
| `-quality` | 85 | JPEG quality (1-100) |
| `-preview` | false | Show local preview |
| `-samplerate` | 44100 | Audio sample rate (Hz) |
| `-node` | http://localhost:26658 | Celestia node URL |
| `-token` | | Auth token (required) |

### View Options

| Option | Default | Description |
|--------|---------|-------------|
| `-namespace` | | Stream namespace (required) |
| `-height` | | Start block height (required) |
| `-node` | http://localhost:26658 | Celestia node URL |
| `-token` | | Auth token (required) |

## How It Works

### Streaming

1. **Entrypoint**: Capturer sends entrypoint blob with stream metadata (sample rate, channels, fps)
2. **Initialize**: Capturer opens webcam (GoCV) and microphone (malgo)
3. **Encode**: Video frames are JPEG encoded, audio is 16-bit PCM via the Encoder interface
4. **Multiplex**: Frames are tagged with VIDF/AUDF markers and timestamps
5. **Chunk**: Data is accumulated into 1MB buffers inside Capturer
6. **Submit**: Blobs are submitted to Celestia under a randomly generated namespace

### Viewing

1. **Viewer** connects to Celestia node
2. **Find Entrypoint**: Locate the ENTR blob with stream parameters
3. **Fetch Blobs**: Poll for new blobs at sequential block heights
4. **Decode**: Parse frame headers, decode JPEG/PCM data via the Decoder interface
5. **Sync**: Use timestamps to synchronize audio and video playback
6. **Display**: Show video in window, play audio through speakers

## Package Structure

```
love/
├── main.go              # CLI entry point
├── lib/
│   ├── capture/         # Video + audio capture with embedded encoder
│   │   ├── capture.go   # Capturer implementation
│   │   └── config.go    # Capture configuration
│   ├── codec/           # Encoding/decoding interfaces and implementations
│   │   ├── interface.go # Encoder/Decoder interfaces
│   │   ├── jpeg.go      # JPEGCodec implementation
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
