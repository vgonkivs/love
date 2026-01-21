// chaintest is a test application to verify the full A/V encode/decode chain
// Uses gomedia for MPEG-TS muxing/demuxing (pure Go) and ffmpeg for H.264/AAC encoding/decoding
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/gen2brain/malgo"
	"gocv.io/x/gocv"

	"github.com/vgonkivs/love/lib/codec"
)

func init() {
	// Lock the main goroutine to the main OS thread for OpenCV GUI on macOS
	runtime.LockOSThread()
}

func main() {
	// Parse flags
	cameraID := flag.Int("camera", 0, "Camera device ID")
	width := flag.Int("width", 640, "Capture width")
	height := flag.Int("height", 480, "Capture height")
	fps := flag.Int("fps", 30, "Frames per second")
	bitrate := flag.String("bitrate", "2M", "H.264 bitrate")
	sampleRate := flag.Int("samplerate", 44100, "Audio sample rate")
	duration := flag.Int("duration", 0, "Test duration in seconds (0 = run until ESC)")
	noAudio := flag.Bool("no-audio", false, "Disable audio capture/playback")
	flag.Parse()

	fmt.Println("=== A/V Chain Test (gomedia MPEG-TS + ffmpeg H.264/AAC) ===")
	fmt.Printf("Camera: %d, Resolution: %dx%d, FPS: %d, Bitrate: %s\n",
		*cameraID, *width, *height, *fps, *bitrate)
	if !*noAudio {
		fmt.Printf("Audio: %d Hz, mono\n", *sampleRate)
	} else {
		fmt.Println("Audio: disabled")
	}
	fmt.Println("Press ESC to exit")
	fmt.Println()

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	// Create H.264 encoder
	h264Cfg := codec.H264EncoderConfig{
		Width:   *width,
		Height:  *height,
		FPS:     *fps,
		Bitrate: *bitrate,
		GOPSize: *fps * 2, // Keyframe every 2 seconds
	}
	h264Encoder := codec.NewH264Encoder(h264Cfg)
	if err := h264Encoder.Start(); err != nil {
		log.Fatalf("Failed to start H.264 encoder: %v", err)
	}
	defer h264Encoder.Close()
	log.Println("H.264 encoder started")

	// Create AAC encoder
	aacEncoder := codec.NewAACEncoder(codec.AACEncoderConfig{
		SampleRate: *sampleRate,
		Channels:   1,
		Bitrate:    "128k",
	})
	if !*noAudio {
		if err := aacEncoder.Start(); err != nil {
			log.Printf("Warning: Failed to start AAC encoder: %v", err)
			*noAudio = true
		} else {
			log.Println("AAC encoder started")
		}
	}
	defer aacEncoder.Close()

	// Create gomedia TS muxer
	tsMuxer := codec.NewTSMuxer(codec.TSMuxerConfig{
		Width:      *width,
		Height:     *height,
		FPS:        *fps,
		SampleRate: *sampleRate,
		Channels:   1,
	})
	if err := tsMuxer.Start(); err != nil {
		log.Fatalf("Failed to start TS muxer: %v", err)
	}
	defer tsMuxer.Close()
	log.Println("MPEG-TS muxer started (gomedia - pure Go)")

	// Create gomedia TS demuxer
	tsDemuxer := codec.NewTSDemuxer(codec.TSDemuxerConfig{
		Width:      *width,
		Height:     *height,
		SampleRate: *sampleRate,
		Channels:   1,
	})
	if err := tsDemuxer.Start(); err != nil {
		log.Fatalf("Failed to start TS demuxer: %v", err)
	}
	defer tsDemuxer.Close()
	log.Println("MPEG-TS demuxer started (gomedia - pure Go)")

	// Create H.264 decoder
	h264Decoder := codec.NewH264Decoder(codec.H264DecoderConfig{
		Width:  *width,
		Height: *height,
	})
	if err := h264Decoder.Start(); err != nil {
		log.Fatalf("Failed to start H.264 decoder: %v", err)
	}
	defer h264Decoder.Close()
	log.Println("H.264 decoder started")

	// Create AAC decoder
	aacDecoder := codec.NewAACDecoder(codec.AACDecoderConfig{
		SampleRate: *sampleRate,
		Channels:   1,
	})
	if !*noAudio {
		if err := aacDecoder.Start(); err != nil {
			log.Printf("Warning: Failed to start AAC decoder: %v", err)
		} else {
			log.Println("AAC decoder started")
		}
	}
	defer aacDecoder.Close()

	// Open camera
	cam, err := gocv.OpenVideoCapture(*cameraID)
	if err != nil {
		log.Fatalf("Failed to open camera: %v", err)
	}
	defer cam.Close()

	// Configure camera
	cam.Set(gocv.VideoCaptureFrameWidth, float64(*width))
	cam.Set(gocv.VideoCaptureFrameHeight, float64(*height))
	cam.Set(gocv.VideoCaptureFPS, float64(*fps))
	cam.Set(gocv.VideoCaptureBufferSize, 1)

	actualWidth := cam.Get(gocv.VideoCaptureFrameWidth)
	actualHeight := cam.Get(gocv.VideoCaptureFrameHeight)
	actualFPS := cam.Get(gocv.VideoCaptureFPS)
	log.Printf("Camera configured: %.0fx%.0f@%.1ffps", actualWidth, actualHeight, actualFPS)

	// Setup audio capture if enabled
	var audioCtx *malgo.AllocatedContext
	var audioDevice *malgo.Device
	var audioBuffer []byte
	var audioMu sync.Mutex
	var audioRunning bool

	if !*noAudio {
		var err error
		audioCtx, err = malgo.InitContext(nil, malgo.ContextConfig{}, nil)
		if err != nil {
			log.Printf("Warning: Failed to init audio context: %v (continuing without audio)", err)
			*noAudio = true
		} else {
			deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
			deviceConfig.Capture.Format = malgo.FormatS16
			deviceConfig.Capture.Channels = 1
			deviceConfig.SampleRate = uint32(*sampleRate)
			deviceConfig.PeriodSizeInFrames = 1024
			deviceConfig.Alsa.NoMMap = 1

			// Samples to accumulate before sending (~50ms worth)
			samplesPerSend := *sampleRate / 20 * 1 * 2

			onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
				audioMu.Lock()
				defer audioMu.Unlock()

				audioBuffer = append(audioBuffer, inputSamples...)

				for len(audioBuffer) >= samplesPerSend {
					chunk := make([]byte, samplesPerSend)
					copy(chunk, audioBuffer[:samplesPerSend])
					audioBuffer = audioBuffer[samplesPerSend:]

					// Write to AAC encoder
					if err := aacEncoder.Write(chunk); err != nil {
						// Ignore write errors during shutdown
					}
				}
			}

			deviceCallbacks := malgo.DeviceCallbacks{
				Data: onRecvFrames,
			}

			audioDevice, err = malgo.InitDevice(audioCtx.Context, deviceConfig, deviceCallbacks)
			if err != nil {
				log.Printf("Warning: Failed to init audio device: %v (continuing without audio)", err)
				audioCtx.Uninit()
				audioCtx.Free()
				*noAudio = true
			} else {
				if err := audioDevice.Start(); err != nil {
					log.Printf("Warning: Failed to start audio device: %v (continuing without audio)", err)
					audioDevice.Uninit()
					audioCtx.Uninit()
					audioCtx.Free()
					*noAudio = true
				} else {
					audioRunning = true
					log.Printf("Audio capture started (%d Hz)", *sampleRate)
				}
			}
		}
	}

	// Cleanup audio on exit
	defer func() {
		if audioRunning {
			audioDevice.Stop()
			audioDevice.Uninit()
			audioCtx.Uninit()
			audioCtx.Free()
			log.Println("Audio capture stopped")
		}
	}()

	// Setup audio playback if audio is enabled
	var playbackCtx *malgo.AllocatedContext
	var playbackDevice *malgo.Device
	var playbackBuffer []byte
	var playbackMu sync.Mutex
	var playbackRunning bool

	if !*noAudio {
		var err error
		playbackCtx, err = malgo.InitContext(nil, malgo.ContextConfig{}, nil)
		if err != nil {
			log.Printf("Warning: Failed to init playback context: %v", err)
		} else {
			deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
			deviceConfig.Playback.Format = malgo.FormatS16
			deviceConfig.Playback.Channels = 1
			deviceConfig.SampleRate = uint32(*sampleRate)
			deviceConfig.PeriodSizeInFrames = 1024
			deviceConfig.Alsa.NoMMap = 1

			onSendFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
				playbackMu.Lock()
				defer playbackMu.Unlock()

				bytesNeeded := int(frameCount) * 1 * 2

				if len(playbackBuffer) >= bytesNeeded {
					copy(outputSamples, playbackBuffer[:bytesNeeded])
					playbackBuffer = playbackBuffer[bytesNeeded:]
				} else {
					copy(outputSamples, playbackBuffer)
					for i := len(playbackBuffer); i < bytesNeeded; i++ {
						outputSamples[i] = 0
					}
					playbackBuffer = playbackBuffer[:0]
				}
			}

			deviceCallbacks := malgo.DeviceCallbacks{
				Data: onSendFrames,
			}

			playbackDevice, err = malgo.InitDevice(playbackCtx.Context, deviceConfig, deviceCallbacks)
			if err != nil {
				log.Printf("Warning: Failed to init playback device: %v", err)
				playbackCtx.Uninit()
				playbackCtx.Free()
			} else {
				if err := playbackDevice.Start(); err != nil {
					log.Printf("Warning: Failed to start playback device: %v", err)
					playbackDevice.Uninit()
					playbackCtx.Uninit()
					playbackCtx.Free()
				} else {
					playbackRunning = true
					log.Printf("Audio playback started (%d Hz)", *sampleRate)
				}
			}
		}
	}

	// Cleanup playback on exit
	defer func() {
		if playbackRunning {
			playbackDevice.Stop()
			playbackDevice.Uninit()
			playbackCtx.Uninit()
			playbackCtx.Free()
			log.Println("Audio playback stopped")
		}
	}()

	// Create display window for decoded output
	window := gocv.NewWindow("A/V Chain Test - Decoded Output")
	defer window.Close()

	frame := gocv.NewMat()
	defer frame.Close()

	frameDuration := time.Second / time.Duration(*fps)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	startTime := time.Now()
	var endTime time.Time
	if *duration > 0 {
		endTime = startTime.Add(time.Duration(*duration) * time.Second)
	}

	frameCount := 0
	muxedBytes := 0
	decodedVideoCount := 0
	decodedAudioChunks := 0

	log.Println("Starting A/V capture/mux/demux/playback loop...")

	for {
		select {
		case <-ctx.Done():
			goto done

		case <-ticker.C:
			// Check duration limit
			if *duration > 0 && time.Now().After(endTime) {
				goto done
			}

			// Capture video frame
			if ok := cam.Read(&frame); !ok {
				log.Println("Failed to read frame")
				continue
			}

			if frame.Empty() {
				continue
			}

			frameCount++
			ptsMs := uint64(time.Since(startTime).Milliseconds())

			// Encode video frame to H.264
			// Get raw frame data and write to encoder
			frameData := frame.ToBytes()
			if _, err := h264Encoder.EncodeVideo(frame, time.Since(startTime), uint32(frameCount)); err != nil {
				log.Printf("H.264 encode error: %v", err)
			}
			_ = frameData // Used implicitly by EncodeVideo

			// Read encoded H.264 NAL units and feed to TS muxer
			for {
				encoded, err := h264Encoder.ReadEncodedFrameTimeout(time.Since(startTime), uint32(frameCount), time.Millisecond)
				if err != nil || encoded == nil {
					break
				}
				// Strip our header (20 bytes) to get raw H.264 data
				if len(encoded) > 20 {
					h264Data := encoded[20:]
					tsMuxer.WriteVideo(h264Data, ptsMs, ptsMs)
				}
			}

			// Read encoded AAC frames and feed to TS muxer
			if !*noAudio {
				for {
					aacFrame := aacEncoder.ReadFrame()
					if aacFrame == nil {
						break
					}
					tsMuxer.WriteAudio(aacFrame, ptsMs)
				}
			}

			// Read muxed MPEG-TS data and feed to demuxer
			tsData := tsMuxer.ReadMuxedData()
			if tsData != nil {
				muxedBytes += len(tsData)
				tsDemuxer.WriteMuxedData(tsData)
			}

			// Read demuxed video packets and decode
			for {
				videoPkt := tsDemuxer.ReadVideoPacket()
				if videoPkt == nil {
					break
				}

				// Feed H.264 data to decoder
				decodedFrame, _ := h264Decoder.DecodeH264Frame(videoPkt.Data)
				if decodedFrame != nil {
					window.IMShow(*decodedFrame)
					decodedFrame.Close()
					decodedVideoCount++
				}
			}

			// Read demuxed audio packets and decode
			if playbackRunning {
				for {
					audioPkt := tsDemuxer.ReadAudioPacket()
					if audioPkt == nil {
						break
					}

					// Feed AAC data to decoder
					aacDecoder.Write(audioPkt.Data)

					// Read decoded PCM and add to playback buffer
					for {
						pcmSamples := aacDecoder.ReadSamples()
						if pcmSamples == nil {
							break
						}
						playbackMu.Lock()
						playbackBuffer = append(playbackBuffer, pcmSamples...)
						playbackMu.Unlock()
						decodedAudioChunks++
					}
				}
			}

			// Check for ESC key
			if window.WaitKey(1) == 27 {
				log.Println("ESC pressed")
				goto done
			}
		}
	}

done:
	elapsed := time.Since(startTime)

	fmt.Println()
	fmt.Println("=== Test Results ===")
	fmt.Printf("Duration: %.1f seconds\n", elapsed.Seconds())
	fmt.Printf("Video frames captured: %d\n", frameCount)
	fmt.Printf("Video frames decoded: %d\n", decodedVideoCount)
	fmt.Printf("Audio chunks decoded: %d\n", decodedAudioChunks)
	fmt.Printf("Total muxed MPEG-TS size: %d bytes\n", muxedBytes)
	if elapsed.Seconds() > 0 {
		fmt.Printf("Effective bitrate: %.2f Mbps\n", float64(muxedBytes*8)/elapsed.Seconds()/1_000_000)
		fmt.Printf("Capture FPS: %.1f\n", float64(frameCount)/elapsed.Seconds())
		if decodedVideoCount > 0 {
			fmt.Printf("Decode FPS: %.1f\n", float64(decodedVideoCount)/elapsed.Seconds())
		}
	}
	fmt.Println("====================")

	// Print ffmpeg stderr if there were issues
	if decodedVideoCount == 0 {
		fmt.Println("\nH.264 encoder stderr:", h264Encoder.GetStderr())
		fmt.Println("\nH.264 decoder stderr:", h264Decoder.GetStderr())
		fmt.Println("\nAAC encoder stderr:", aacEncoder.GetStderr())
		fmt.Println("\nAAC decoder stderr:", aacDecoder.GetStderr())
	}
}
