package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/vgonkivs/love/lib/capture"
	"github.com/vgonkivs/love/lib/codec"
	"github.com/vgonkivs/love/lib/streamer"
	"github.com/vgonkivs/love/lib/viewer"
)

func init() {
	// Lock the main goroutine to the main OS thread.
	// This MUST happen before any other code runs because macOS
	// requires all Cocoa/AppKit UI operations (including OpenCV windows)
	// to occur on thread 0.
	runtime.LockOSThread()
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "stream":
		runStream(os.Args[2:])
	case "view":
		runView(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("LOVE - Live Onchain Video Environment")
	fmt.Println("Stream live video to Celestia blockchain")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  love stream [options]   - Start streaming from webcam to Celestia")
	fmt.Println("  love view [options]     - View stream from Celestia")
	fmt.Println()
	fmt.Println("Stream options:")
	fmt.Println("  -camera int        Camera device ID (default 0)")
	fmt.Println("  -width int         Capture width (default 1280)")
	fmt.Println("  -height int        Capture height (default 720)")
	fmt.Println("  -fps int           Frames per second (default 30)")
	fmt.Println("  -quality int       JPEG quality 1-100 (default 85)")
	fmt.Println("  -preview           Enable local preview window")
	fmt.Println("  -samplerate int    Audio sample rate in Hz (default 44100)")
	fmt.Println("  -node string       Celestia node URL (default http://localhost:26658)")
	fmt.Println("  -token string      Celestia node auth token")
	fmt.Println()
	fmt.Println("View options:")
	fmt.Println("  -namespace string  Stream namespace (hex)")
	fmt.Println("  -height uint       Start block height")
	fmt.Println("  -node string       Celestia node URL (default http://localhost:26658)")
	fmt.Println("  -token string      Celestia node auth token")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  love stream -token <auth_token>")
	fmt.Println("  love view -namespace 0a1b2c... -height 1234567 -token <auth_token>")
}

func runStream(args []string) {
	fs := flag.NewFlagSet("stream", flag.ExitOnError)

	// Capture options
	cameraID := fs.Int("camera", 0, "Camera device ID")
	width := fs.Int("width", 1280, "Capture width")
	height := fs.Int("height", 720, "Capture height")
	fps := fs.Int("fps", 30, "Frames per second")
	preview := fs.Bool("preview", false, "Enable local preview window")

	// Audio options
	sampleRate := fs.Int("samplerate", 44100, "Audio sample rate in Hz")

	// Codec options
	quality := fs.Int("quality", 85, "JPEG quality (1-100)")

	// Celestia options
	nodeURL := fs.String("node", "http://localhost:26658", "Celestia node URL")
	authToken := fs.String("token", "", "Celestia node auth token")

	fs.Parse(args)

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

	// Create codec (encoder)
	encoder := codec.NewJPEGCodec(*quality)

	// Create capturer with encoder
	captureCfg := &capture.Config{
		DeviceID:          *cameraID,
		Width:             *width,
		Height:            *height,
		FPS:               *fps,
		EnablePreview:     *preview,
		PreviewWindowName: "Stream Preview (Local)",
		AudioDeviceID:     -1, // default audio input
		SampleRate:        *sampleRate,
		Channels:          1, // mono
		AudioBuffer:       1024,
	}
	capturer := capture.NewCapturer(captureCfg, encoder)

	// Create streamer
	streamerCfg := &streamer.Config{
		NodeURL:   *nodeURL,
		AuthToken: *authToken,
	}
	str, err := streamer.NewStreamer(streamerCfg)
	if err != nil {
		log.Fatalf("Failed to create streamer: %v", err)
	}

	// Connect to Celestia
	log.Println("Connecting to Celestia node...")
	if err := str.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect to Celestia: %v", err)
	}
	defer str.Close()

	// Create blob channel
	blobChannel := make(chan []byte, 100)

	// Start streamer in goroutine
	log.Printf("Starting streamer...")
	go func() {
		if err := str.Run(ctx, blobChannel); err != nil {
			log.Printf("Streamer error: %v", err)
		}
	}()

	// Run capturer on main thread (required for OpenCV GUI on macOS)
	log.Printf("Starting capturer (camera %d, %dx%d, %dfps, audio %dHz)...",
		*cameraID, *width, *height, *fps, *sampleRate)
	if err := capturer.Run(ctx, blobChannel); err != nil {
		log.Printf("Capturer error: %v", err)
	}
	close(blobChannel)

	log.Println("Stream ended")
}

func runView(args []string) {
	fs := flag.NewFlagSet("view", flag.ExitOnError)

	// Required options
	namespace := fs.String("namespace", "", "Stream namespace (hex)")
	startHeight := fs.Uint64("height", 0, "Start block height")

	// Celestia options
	nodeURL := fs.String("node", "http://localhost:26658", "Celestia node URL")
	authToken := fs.String("token", "", "Celestia node auth token")

	fs.Parse(args)

	if *namespace == "" {
		fmt.Println("Error: -namespace is required")
		fs.PrintDefaults()
		os.Exit(1)
	}
	if *startHeight == 0 {
		fmt.Println("Error: -height is required")
		fs.PrintDefaults()
		os.Exit(1)
	}

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

	// Create codec (decoder)
	decoder := codec.NewJPEGCodec(85) // quality doesn't matter for decoding

	// Create viewer with decoder
	viewerCfg := &viewer.Config{
		NodeURL:    *nodeURL,
		AuthToken:  *authToken,
		BufferSize: 10,
		WindowName: "Celestia Live Stream",
		PollDelay:  500 * time.Millisecond,
	}

	v, err := viewer.NewViewer(viewerCfg, decoder, *namespace, *startHeight)
	if err != nil {
		log.Fatalf("Failed to create viewer: %v", err)
	}

	// Connect to Celestia
	log.Println("Connecting to Celestia node...")
	if err := v.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect to Celestia: %v", err)
	}
	defer v.Close()

	log.Printf("Subscribing to namespace at height %d...", *startHeight)
	log.Println("Press ESC to exit")

	if err := v.Run(ctx); err != nil {
		log.Printf("Viewer error: %v", err)
	}

	log.Println("Viewer stopped")
}
