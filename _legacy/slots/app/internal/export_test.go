package internal

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// What pairing_test.go, an external test, reaches of this package. It composes
// gfx, canvas, ui and input, whose roots import app's root, which imports this
// package, so it cannot be a test of this package itself.

type FakeMainLoop = fakeMainLoop

type PermanentAdapter = permanentAdapter

// The app_time tool's request and response.
type (
	TimeToolRequest  = timeToolRequest
	TimeToolResponse = timeToolResponse
)

// Attached is the Loop app handed the fake main loop, or nil.
func (d *fakeMainLoop) Attached() Loop { return d.attached() }

// NewMainLoopAdapter provides mainLoop as app's MainLoop.
func NewMainLoopAdapter(mainLoop *FakeMainLoop) kernel.Plugin { return mainLoopAdapter{mainLoop} }

func TickTestConfig() Config { return tickTestConfig() }

// WaitSharing is waitSharing on the tick source of app, a plugin New made.
func WaitSharing(t *testing.T, app kernel.Plugin, callers int64, within time.Duration) {
	waitSharing(t, &app.(*plugin).loop.ticks, callers, within)
}
