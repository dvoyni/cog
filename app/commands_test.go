package app

import (
	"context"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// Paused is the caller-side half of TimeCmd, and the answer worth pinning is
// the one for an engine that never had a tick source to stop: every
// frame-bound capability reads it before it decides to wait, and a fallback
// the wrong way round is a wait for a tick nobody will ever publish.

// tickSource answers TimeCmd the way the host that owns the loop does, with
// the state the test set. Package app declares the command and implements
// nothing, so a test of the caller's half has to bring its own driver.
type tickSource struct{ paused bool }

func (*tickSource) Name() kernel.PluginName           { return "apptesttime" }
func (*tickSource) Dependencies() []kernel.PluginName { return nil }

func (s *tickSource) Register(r *kernel.Registrar, _ any) error {
	r.HandleCommand[TimeCmd](s.timeCmdImpl)
	return nil
}

func (s *tickSource) timeCmdImpl() (kernel.Lock, kernel.Execute[TimeRequest, TimeResponse]) {
	return nil, func(kernel.Kernel, TimeRequest) (TimeResponse, error) {
		return TimeResponse{Paused: s.paused}, nil
	}
}

// runEngine starts an engine over the given plugins and hands back the
// Executioner a capability body holds.
func runEngine(t *testing.T, plugins ...kernel.Plugin) kernel.Executioner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	engine := kernel.New(nil).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(plugins...)
	stopped := make(chan struct{})
	go func() { engine.Run(ctx); close(stopped) }()
	<-engine.Ready()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	return engine.Executioner()
}

// An engine with a tick source answers for it, either way round.
func TestPausedReportsTheTickSource(t *testing.T) {
	for _, one := range []struct {
		name   string
		paused bool
	}{
		{name: "paused", paused: true},
		{name: "running", paused: false},
	} {
		t.Run(one.name, func(t *testing.T) {
			k := runEngine(t, &tickSource{paused: one.paused})

			if got := Paused(k); got != one.paused {
				t.Errorf("Paused = %v, want %v", got, one.paused)
			}
		})
	}
}

// A game composed without time control cannot be paused, so the dispatch
// failing is an answer rather than a fault: it is running.
func TestPausedTreatsAnEngineWithoutTimeControlAsRunning(t *testing.T) {
	k := runEngine(t)

	// Asserted rather than assumed: without this the test would pass just as
	// well against an engine that did handle the command and said running.
	if _, err := k.ExecuteCommand[TimeCmd](TimeRequest{Action: TimeStatus}); err == nil {
		t.Fatal("the engine handled TimeCmd, so this is not the fallback")
	}
	if Paused(k) {
		t.Error("an engine with no tick source to stop reported itself paused")
	}
}
