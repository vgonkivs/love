# LOVE

**L**ive **O**nchain **V**ideo **E**nvironment

---

## What is LOVE?

LOVE is a decentralized live streaming platform built on Celestia's data availability layer. It captures video and audio from your webcam/microphone, encodes and multiplexes them into 1MB data chunks, and submits them as blobs to the Celestia blockchain. Viewers can then fetch these blobs and play back the stream in real-time with synchronized audio and video.

## Features

- **Live Streaming**: Real-time video capture from webcam with configurable resolution and framerate
- **Audio Support**: Synchronized audio capture from microphone (16-bit PCM, 44.1kHz)
- **On-chain Storage**: All stream data stored as Celestia blobs
- **A/V Sync**: Timestamps and sequence numbers ensure proper audio/video synchronization
- **Decentralized**: No central server - streams go directly to the blockchain
- **Censorship Resistant**: Once on-chain, streams cannot be removed

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
# Video only
./love stream -token <auth_token>

# Video + Audio
./love stream -audio -token <auth_token>

# With local preview window
./love stream -audio -preview -token <auth_token>

# Custom settings
./love stream -audio -width 1920 -height 1080 -fps 30 -quality 90 -token <auth_token>
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
# Video only
./love view -namespace <namespace> -height <start_height> -token <auth_token>

# Video + Audio
./love view -audio -namespace <namespace> -height <start_height> -token <auth_token>
```

Press **ESC** to exit.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              STREAMING                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────────────────┐  │
│   │  Webcam  │───▶│ Capture  │───▶│          │    │                      │  │
│   └──────────┘    │ (GoCV)   │    │          │    │      Streamer        │  │
│                   └──────────┘    │  Codec   │───▶│                      │  │
│   ┌──────────┐    ┌──────────┐    │          │    │  - Random namespace  │  │
│   │   Mic    │───▶│ Capture  │───▶│          │    │  - Batch submission  │  │
│   └──────────┘    │ (malgo)  │    └──────────┘    │  - Entrypoint blob   │  │
│                   └──────────┘                    └──────────┬───────────┘  │
│                                                              │              │
└──────────────────────────────────────────────────────────────┼──────────────┘
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
┌──────────────────────────────────────────────────────────────┼──────────────┐
│                              VIEWING                         │              │
├──────────────────────────────────────────────────────────────┼──────────────┤
│                                                              ▼              │
│   ┌──────────────────────┐    ┌──────────┐    ┌──────────────────────┐     │
│   │       Viewer         │───▶│ Decoder  │───▶│   Display (GoCV)     │     │
│   │                      │    │          │    └──────────────────────┘     │
│   │  - Fetch blobs       │    │          │    ┌──────────────────────┐     │
│   │  - Reorder by seq    │    │          │───▶│   Audio Player       │     │
│   │  - A/V sync timing   │    │          │    │   (malgo)            │     │
│   └──────────────────────┘    └──────────┘    └──────────────────────┘     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
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

### Entrypoint Blob (Audio Streams)

When audio is enabled, an entrypoint blob is submitted first:

```
┌───────────┬─────────────┬──────────┬─────────┐
│  Marker   │ Sample Rate │ Channels │   FPS   │
│  4 bytes  │   4 bytes   │  1 byte  │ 1 byte  │
├───────────┼─────────────┼──────────┼─────────┤
│  "ENTR"   │   44100     │    1     │   30    │
└───────────┴─────────────┴──────────┴─────────┘
```

### Sequenced Blobs

For async streaming, each blob is prefixed with an 8-byte sequence number to ensure correct ordering during playback.

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
| `-audio` | false | Enable audio |
| `-samplerate` | 44100 | Audio sample rate (Hz) |
| `-node` | http://localhost:26658 | Celestia node URL |
| `-token` | | Auth token (required) |

### View Options

| Option | Default | Description |
|--------|---------|-------------|
| `-namespace` | | Stream namespace (required) |
| `-height` | | Start block height (required) |
| `-audio` | false | Enable audio playback |
| `-node` | http://localhost:26658 | Celestia node URL |
| `-token` | | Auth token (required) |

## How It Works

### Streaming

1. **Capture**: GoCV grabs frames from webcam, malgo captures audio from microphone
2. **Encode**: Video frames are JPEG encoded, audio is 16-bit PCM
3. **Multiplex**: Frames are tagged with VIDF/AUDF markers, timestamps, and sequence numbers
4. **Chunk**: Data is accumulated into 1MB buffers
5. **Submit**: Blobs are submitted to Celestia under a randomly generated namespace

### Viewing

1. **Connect**: Viewer connects to Celestia node
2. **Find Entrypoint**: For audio streams, locate the ENTR blob with stream parameters
3. **Fetch Blobs**: Poll for new blobs at sequential block heights
4. **Reorder**: Sort incoming blobs by sequence number
5. **Decode**: Parse frame headers, decode JPEG/PCM data
6. **Sync**: Use timestamps to synchronize audio and video playback
7. **Display**: Show video in window, play audio through speakers

## License

MIT

---

*Go live with LOVE*
