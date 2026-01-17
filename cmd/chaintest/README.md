# A/V Encode/Decode Chain Test

This test application verifies the full H.264 video + AAC audio encode/decode pipeline using:
- **gomedia** for MPEG-TS muxing/demuxing (pure Go)
- **ffmpeg** for H.264/AAC encoding/decoding (via pipes)

No Celestia required.

## What it does

1. Captures video frames from your webcam (gocv)
2. Captures audio from your microphone (malgo)
3. Encodes video to H.264 and audio to AAC (ffmpeg pipes)
4. Muxes into MPEG-TS stream (gomedia - pure Go)
5. Demuxes the MPEG-TS back into H.264/AAC (gomedia - pure Go)
6. Decodes video and audio (ffmpeg pipes)
7. Displays video and plays audio

## Build

```bash
cd /path/to/love
go build -o chaintest ./cmd/chaintest/
```

## Usage

```bash
# Default settings (640x480 @ 30fps with audio)
./chaintest

# Disable audio (video only)
./chaintest -no-audio

# Higher resolution
./chaintest -width 1280 -height 720

# Custom bitrate
./chaintest -bitrate 4M

# Run for specific duration
./chaintest -duration 10
```

## Options

| Option | Default | Description |
|--------|---------|-------------|
| `-camera` | 0 | Camera device ID |
| `-width` | 640 | Capture width in pixels |
| `-height` | 480 | Capture height in pixels |
| `-fps` | 30 | Frames per second |
| `-bitrate` | 2M | H.264 encoding bitrate |
| `-samplerate` | 44100 | Audio sample rate in Hz |
| `-duration` | 0 | Test duration in seconds (0 = run until ESC) |
| `-no-audio` | false | Disable audio capture/playback |

## Controls

- Press **ESC** to exit the test

## Output

```
=== Test Results ===
Duration: 10.0 seconds
Video frames captured: 300
Video frames decoded: 295
Audio chunks decoded: 42
Total muxed MPEG-TS size: 1234567 bytes
Effective bitrate: 0.99 Mbps
Capture FPS: 30.0
Decode FPS: 29.5
====================
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    CAPTURE + ENCODE                              │
│  ┌─────────┐     ┌──────────────────┐                           │
│  │ Webcam  │────▶│ H.264 Encoder    │────┐                      │
│  │ (gocv)  │     │ (ffmpeg pipe)    │    │                      │
│  └─────────┘     └──────────────────┘    │                      │
│  ┌─────────┐     ┌──────────────────┐    │                      │
│  │   Mic   │────▶│ AAC Encoder      │────┤                      │
│  │ (malgo) │     │ (ffmpeg pipe)    │    │                      │
│  └─────────┘     └──────────────────┘    │                      │
└──────────────────────────────────────────┼──────────────────────┘
                                           │
                                           ▼
                           ┌───────────────────────────┐
                           │    MPEG-TS Muxer          │
                           │    (gomedia - pure Go)    │
                           └─────────────┬─────────────┘
                                         │
                                         ▼
                           ┌───────────────────────────┐
                           │    MPEG-TS Demuxer        │
                           │    (gomedia - pure Go)    │
                           └─────────────┬─────────────┘
                                         │
                                         ▼
┌────────────────────────────────────────┴────────────────────────┐
│                         DECODE + PLAYBACK                        │
│  ┌──────────────────┐     ┌─────────┐                           │
│  │ H.264 Decoder    │────▶│ Display │                           │
│  │ (ffmpeg pipe)    │     │ (gocv)  │                           │
│  └──────────────────┘     └─────────┘                           │
│  ┌──────────────────┐     ┌─────────┐                           │
│  │ AAC Decoder      │────▶│ Speaker │                           │
│  │ (ffmpeg pipe)    │     │ (malgo) │                           │
│  └──────────────────┘     └─────────┘                           │
└─────────────────────────────────────────────────────────────────┘
```

## Dependencies

- OpenCV 4.x with GoCV bindings
- ffmpeg (for H.264/AAC encoding/decoding)
- malgo (for audio capture/playback)
- gomedia (pure Go MPEG-TS muxing/demuxing)
