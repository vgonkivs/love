package capture

import (
	"context"
	"log"
	"time"

	"gocv.io/x/gocv"
)

// Capturer captures video frames from webcam and sends them to a channel
type Capturer struct {
	ctx    context.Context
	cancel context.CancelFunc

	cfg *Config
	cam *gocv.VideoCapture
}

// NewCapturer creates a new video capturer
func NewCapturer(cfg *Config) *Capturer {
	return &Capturer{
		cfg: cfg,
	}
}

// Run starts capturing frames and sends them to the output channel.
// This method blocks until context is cancelled or an error occurs.
// IMPORTANT: Must be called from the main goroutine (the one running main())
// because OpenCV GUI operations require the main OS thread on macOS.
// Ensure runtime.LockOSThread() is called in init() before main() runs.
func (c *Capturer) Run(ctx context.Context, output chan<- gocv.Mat) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	cam, err := gocv.OpenVideoCapture(c.cfg.DeviceID)
	if err != nil {
		return err
	}
	c.cam = cam
	defer cam.Close()

	// Configure camera
	cam.Set(gocv.VideoCaptureFrameWidth, float64(c.cfg.Width))
	cam.Set(gocv.VideoCaptureFrameHeight, float64(c.cfg.Height))
	cam.Set(gocv.VideoCaptureFPS, float64(c.cfg.FPS))
	cam.Set(gocv.VideoCaptureBufferSize, 1)

	// Log actual settings
	actualWidth := cam.Get(gocv.VideoCaptureFrameWidth)
	actualHeight := cam.Get(gocv.VideoCaptureFrameHeight)
	actualFPS := cam.Get(gocv.VideoCaptureFPS)

	log.Printf("Capturer: camera configured %.0fx%.0f@%.1ffps (requested: %dx%d@%dfps)",
		actualWidth, actualHeight, actualFPS,
		c.cfg.Width, c.cfg.Height, c.cfg.FPS)

	// Setup preview window if enabled
	var previewWindow *gocv.Window
	if c.cfg.EnablePreview {
		previewWindow = gocv.NewWindow(c.cfg.PreviewWindowName)
		defer previewWindow.Close()
		log.Printf("Capturer: local preview enabled")
	}

	frame := gocv.NewMat()
	defer frame.Close()

	frameDuration := time.Second / time.Duration(c.cfg.FPS)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	log.Printf("Capturer: starting capture loop at %d FPS", c.cfg.FPS)

	for {
		select {
		case <-c.ctx.Done():
			log.Println("Capturer: stopping")
			return nil

		case <-ticker.C:
			if ok := cam.Read(&frame); !ok {
				log.Println("Capturer: failed to read frame")
				continue
			}

			if frame.Empty() {
				continue
			}

			// Display frame in preview window if enabled
			if previewWindow != nil {
				previewWindow.IMShow(frame)
				if previewWindow.WaitKey(1) == 27 { // ESC key
					log.Println("Capturer: preview window closed by user")
					return nil
				}
			}

			// Clone frame and send to channel
			// The receiver is responsible for closing the cloned Mat
			select {
			case output <- frame.Clone():
			case <-c.ctx.Done():
				return nil
			}
		}
	}
}
