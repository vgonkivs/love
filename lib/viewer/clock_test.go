package viewer

import (
	"testing"
	"time"
)

func TestAVClock_NotAnchoredReportsUnusable(t *testing.T) {
	c := newAVClock(44100)
	if _, ok := c.currentMs(); ok {
		t.Fatal("currentMs must report ok=false before anchor")
	}
	// Advancing samples without an anchor must also stay unusable —
	// otherwise the playback callback firing before any audio chunk
	// is queued would silently produce bogus times.
	c.addSamples(1024)
	if _, ok := c.currentMs(); ok {
		t.Fatal("currentMs must stay unusable until anchor is set")
	}
}

func TestAVClock_AnchorAndAdvance(t *testing.T) {
	c := newAVClock(48000)
	c.anchor(1000) // media-time origin at 1000 ms

	got, ok := c.currentMs()
	if !ok {
		t.Fatal("currentMs should be ok after anchor")
	}
	if got != 1000 {
		t.Errorf("zero samples after anchor: got %d ms, want 1000 ms", got)
	}

	// 24000 samples at 48kHz == 500 ms of playback.
	c.addSamples(24000)
	got, _ = c.currentMs()
	if got != 1500 {
		t.Errorf("after 500ms of samples: got %d ms, want 1500 ms", got)
	}

	// Re-anchoring is a no-op so an underrun cannot rewind the clock.
	c.anchor(9999)
	got, _ = c.currentMs()
	if got != 1500 {
		t.Errorf("anchor() after first call must be no-op: got %d ms, want 1500 ms", got)
	}
}

func TestAVClock_RateMustBePositive(t *testing.T) {
	c := newAVClock(0)
	c.anchor(0)
	if _, ok := c.currentMs(); ok {
		t.Fatal("currentMs must report ok=false when rate is 0")
	}
	c.setRate(44100)
	if _, ok := c.currentMs(); !ok {
		t.Fatal("currentMs should be ok once rate is set and clock is anchored")
	}
}

func TestDecideVideoPace(t *testing.T) {
	const ahead = 250 * time.Millisecond
	const behind = 100 * time.Millisecond

	cases := []struct {
		name     string
		framePTS uint64
		mediaNow uint64
		want     paceDecision
	}{
		{
			name:     "on-time frame displays immediately",
			framePTS: 1000, mediaNow: 1000,
			want: paceDecision{},
		},
		{
			name:     "small jitter inside tolerance displays",
			framePTS: 1050, mediaNow: 1000,
			want: paceDecision{},
		},
		{
			name:     "far-future frame sleeps until target",
			framePTS: 2000, mediaNow: 1000,
			want: paceDecision{sleep: 1000 * time.Millisecond},
		},
		{
			name:     "far-past frame drops",
			framePTS: 1000, mediaNow: 2000,
			want: paceDecision{drop: true},
		},
		{
			name:     "exactly at maxAhead displays (boundary)",
			framePTS: 1250, mediaNow: 1000,
			want: paceDecision{},
		},
		{
			name:     "just past maxAhead sleeps",
			framePTS: 1251, mediaNow: 1000,
			want: paceDecision{sleep: 251 * time.Millisecond},
		},
		{
			name:     "exactly at maxBehind displays (boundary)",
			framePTS: 1000, mediaNow: 1100,
			want: paceDecision{},
		},
		{
			name:     "just past maxBehind drops",
			framePTS: 1000, mediaNow: 1101,
			want: paceDecision{drop: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideVideoPace(tc.framePTS, tc.mediaNow, ahead, behind)
			if got != tc.want {
				t.Errorf("decideVideoPace(pts=%d, now=%d) = %+v, want %+v",
					tc.framePTS, tc.mediaNow, got, tc.want)
			}
		})
	}
}
