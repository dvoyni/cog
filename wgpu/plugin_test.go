package wgpu

import (
	"math"
	"testing"
	"time"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func newAccumPlugin(step, maxFrame time.Duration, maxPending int) *Plugin {
	return &Plugin{config: DefaultConfig().
		WithStep(step).
		WithMaxFrame(maxFrame).
		WithMaxPending(maxPending)}
}

// accumulate returns one step per whole fixed step and leaves the remainder as alpha.
func TestOnUpdateEnqueuesWholeSteps(t *testing.T) {
	p := newAccumPlugin(10*time.Millisecond, time.Second, 8)

	if got := p.accumulate(0.035); got != 3 { // 3.5 steps of 10ms
		t.Fatalf("steps = %d, want 3", got)
	}
	if a := p.loadAlpha(); !almostEqual(a, 0.5) {
		t.Fatalf("alpha = %v, want 0.5", a)
	}
}

// A long stall is clamped to MaxFrame so catch-up work stays bounded.
func TestOnUpdateClampsToMaxFrame(t *testing.T) {
	p := newAccumPlugin(10*time.Millisecond, 20*time.Millisecond, 8)

	if got := p.accumulate(1.0); got != 2 { // huge stall, clamped to 20ms -> 2 steps
		t.Fatalf("steps = %d, want 2 (clamped)", got)
	}
	if a := p.loadAlpha(); !almostEqual(a, 0) {
		t.Fatalf("alpha = %v, want 0", a)
	}
}

// When catch-up exceeds MaxPending, extra steps are dropped but accum is still
// drained (drop-to-real-time).
func TestOnUpdateDropsWhenFull(t *testing.T) {
	p := newAccumPlugin(10*time.Millisecond, time.Second, 2)

	if got := p.accumulate(0.055); got != 2 { // 5.5 steps, only 2 published -> 3 dropped
		t.Fatalf("steps = %d, want 2 (rest dropped)", got)
	}
	if a := p.loadAlpha(); !almostEqual(a, 0.5) {
		t.Fatalf("alpha = %v, want 0.5", a)
	}
}

// Sub-step frame times accumulate across calls until a whole step is reached.
func TestOnUpdateAccumulatesAcrossCalls(t *testing.T) {
	p := newAccumPlugin(10*time.Millisecond, time.Second, 8)

	if got := p.accumulate(0.006); got != 0 { // 0.6 step, no tick yet
		t.Fatalf("steps = %d, want 0", got)
	}
	if got := p.accumulate(0.006); got != 1 { // total 1.2 steps -> one tick, 0.2 left
		t.Fatalf("steps = %d, want 1", got)
	}
	if a := p.loadAlpha(); !almostEqual(a, 0.2) {
		t.Fatalf("alpha = %v, want 0.2", a)
	}
}
