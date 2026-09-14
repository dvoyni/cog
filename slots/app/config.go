package app

import "time"

// Config configures the fixed-step loop. It is supplied under Name, and its
// zero value is the default: a zero field takes the default its comment names,
// so a caller sets only what it changes, directly or with the With* builders
// (each returns a modified copy):
//
//	config := map[kernel.PluginName]any{app.Name: app.Config{}.WithStep(time.Second / 30)}
type Config struct {
	// Step is the fixed simulation interval, the update rate. Zero means 1/60s.
	Step time.Duration
	// MaxFrame clamps the real time absorbed in a single frame, bounding
	// catch-up work after a stall (anti spiral-of-death). Zero means 250ms.
	MaxFrame time.Duration
	// MaxPending bounds how many catch-up steps one frame publishes; the whole
	// steps beyond it are dropped. Zero means 4.
	MaxPending int
}

// WithStep sets the fixed simulation interval.
func (c Config) WithStep(step time.Duration) Config {
	c.Step = step
	return c
}

// WithMaxFrame sets the per-frame elapsed-time clamp.
func (c Config) WithMaxFrame(d time.Duration) Config {
	c.MaxFrame = d
	return c
}

// WithMaxPending sets the catch-up cap.
func (c Config) WithMaxPending(n int) Config {
	c.MaxPending = n
	return c
}
