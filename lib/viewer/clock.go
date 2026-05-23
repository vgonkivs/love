package viewer

import (
	"sync"
	"time"
)

// Tolerances for video-vs-audio pacing. videoMaxAhead is how far in
// the future a frame may be before we sleep — 250 ms is roughly 7.5
// frames at 30 fps, big enough that ordinary jitter does not trigger
// a sleep, small enough that real desync is caught quickly.
// videoMaxBehind is the past-tolerance before we drop — 100 ms keeps
// audio lip-sync usable even when the decoder is briefly slow.
const (
	videoMaxAhead  = 250 * time.Millisecond
	videoMaxBehind = 100 * time.Millisecond
)

// avClock derives a monotonic media-time from the audio device's own
// sample counter. When the playback callback emits N samples per
// channel, the clock advances by N / sampleRate seconds — independent
// of wall-clock drift between the system clock and the audio crystal.
//
// Video pacing reads currentMs() to decide whether each decoded frame
// is early (sleep), on-time (display), or late (drop). This is the
// foundation R2 needs to keep A and V locked together over long runs.
type avClock struct {
	mu       sync.Mutex
	rate     int    // sample rate (Hz)
	anchored bool   // true once an audio chunk has been queued
	anchorMs uint64 // PTS of the first audio chunk (ms)
	samples  uint64 // samples per channel actually emitted by the device
}

// newAVClock returns a clock pre-configured with the playback sample rate.
// A rate of zero leaves the clock unusable — currentMs() will report
// ok=false until both the rate is set and an anchor PTS arrives.
func newAVClock(rate int) *avClock {
	return &avClock{rate: rate}
}

// setRate updates the sample rate. Callers that learn the rate after
// constructing the clock (e.g. via the stream entrypoint) use this.
func (c *avClock) setRate(rate int) {
	c.mu.Lock()
	c.rate = rate
	c.mu.Unlock()
}

// anchor records the PTS of the first audio chunk played. Subsequent
// calls are no-ops so re-anchoring after underrun does not jump the
// clock backward. Idempotency keeps the playback loop simple — it can
// call anchor() on every audio frame without special-casing the first.
func (c *avClock) anchor(ptsMs uint64) {
	c.mu.Lock()
	if !c.anchored {
		c.anchorMs = ptsMs
		c.anchored = true
	}
	c.mu.Unlock()
}

// addSamples advances the clock by n samples per channel. Call this
// with the count actually copied to the device, not the count
// requested — silence-fill during underrun must not advance the clock
// or video will race past the buffered audio.
func (c *avClock) addSamples(n uint64) {
	if n == 0 {
		return
	}
	c.mu.Lock()
	c.samples += n
	c.mu.Unlock()
}

// currentMs returns the current media-time in milliseconds. ok=false
// means the clock is not usable yet (no anchor, or rate not set);
// callers must fall back to wall-clock pacing in that case.
func (c *avClock) currentMs() (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.anchored || c.rate <= 0 {
		return 0, false
	}
	elapsedMs := c.samples * 1000 / uint64(c.rate)
	return c.anchorMs + elapsedMs, true
}

// paceDecision is the verdict for one decoded video frame, expressed in
// terms of what the playback loop should do next.
type paceDecision struct {
	// sleep is the duration to wait before displaying. Zero means
	// display immediately.
	sleep time.Duration
	// drop is true if the frame is too far behind the audio clock
	// to be worth displaying; the caller should release the mat and
	// move on without showing it.
	drop bool
}

// decideVideoPace returns the action for a video frame given the
// current media-time. maxAhead is how far in the future a frame may
// be before we sleep; maxBehind is the past-tolerance before we drop.
//
// The math uses signed arithmetic because framePTSms < mediaNowMs is
// the late-frame case and uint subtraction would wrap around.
func decideVideoPace(framePTSms, mediaNowMs uint64, maxAhead, maxBehind time.Duration) paceDecision {
	diffMs := int64(framePTSms) - int64(mediaNowMs)
	if diffMs > maxAhead.Milliseconds() {
		return paceDecision{sleep: time.Duration(diffMs) * time.Millisecond}
	}
	if diffMs < -maxBehind.Milliseconds() {
		return paceDecision{drop: true}
	}
	return paceDecision{}
}
