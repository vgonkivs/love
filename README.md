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
git clone https://github.com/vgonkivs/blob.git
cd blob
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
