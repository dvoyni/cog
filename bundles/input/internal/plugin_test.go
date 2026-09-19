package internal

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

type probeCmd kernel.Command[probeRequest, probeResponse]
type probeRequest struct{}
type probeResponse struct{ Pressed, Just bool }

type probePlugin struct{ keyc chan<- input.KeyEvent }

type testKeyEventHandler kernel.Subscription[input.KeyEvent]

func (probePlugin) Name() kernel.PluginName { return "test" }

// Name is the input plugin's, not this fixture's: the probe locks input.State.
func (probePlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{input.Name} }
func (p probePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[probeCmd](probeCmdImpl)
	registrar.Subscribe[testKeyEventHandler](func() (kernel.Lock, kernel.Observe[input.KeyEvent]) {
		return nil, func(_ kernel.Kernel, event input.KeyEvent) {
			p.keyc <- event
		}
	})
	return nil
}

func probeCmdImpl() (kernel.Lock, kernel.Execute[probeRequest, probeResponse]) {
	var state kernel.Read[*input.State]
	return func(access kernel.ResourceAccess) {
			state = access.GetRead[*input.State]()
		}, func(kernel.Kernel, probeRequest) probeResponse {
			s := state.Get()
			return probeResponse{Pressed: s.Pressed(input.KeyA), Just: s.JustPressed(input.KeyA)}
		}
}

// End-to-end: Apply folds a key press into State and publishes KeyEvent; an
// app.UpdateEvent rolls the edge so JustPressed shows up when polled.
func TestInputPluginApplyPollAndEvent(t *testing.T) {

	keyc := make(chan input.KeyEvent, 8)
	engine := kernel.New(nil).Handler(func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	}).WithPlugins(New(), probePlugin{keyc: keyc})
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	k := engine.Executioner()
	// Apply a key-down batch.
	k.ExecuteCommand[input.ApplyCmd](input.ApplyRequest{Changes: []input.Change{input.KeyChange(input.KeyA, input.ModShift, true)}})

	// The discrete KeyEvent should have been published.
	select {
	case p := <-keyc:
		if p.Key != input.KeyA || !p.Down || !p.Mods.Has(input.ModShift) {
			t.Fatalf("KeyEvent = %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for input.KeyEvent")
	}

	// Before a tick, JustPressed is not yet rolled in.
	if resp := probe(t, k); resp.Just {
		t.Fatal("JustPressed should be false before a tick")
	}

	// A tick rolls the per-tick edges.
	k.PublishEvent(app.UpdateEvent{Dt: 0.016}).Wait()
	if resp := probe(t, k); !resp.Pressed || !resp.Just {
		t.Fatalf("after tick: Pressed=%v Just=%v, want true/true", resp.Pressed, resp.Just)
	}
}

func probe(t *testing.T, k kernel.Executioner) probeResponse {
	t.Helper()
	response := k.ExecuteCommand[probeCmd](probeRequest{})
	return response
}
