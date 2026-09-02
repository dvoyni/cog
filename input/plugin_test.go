package input

import (
	"context"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

type probeCmd kernel.Command[probeRequest, probeResponse]
type probeRequest struct{}
type probeResponse struct{ Pressed, Just bool }

type probePlugin struct{ keyc chan<- KeyEvent }

type testKeyEventHandler kernel.Subscription[KeyEvent]

func (probePlugin) Name() kernel.PluginName { return "test" }

// Name is the input plugin's, not this fixture's: the probe locks input.State.
func (probePlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }
func (p probePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[probeCmd](probeCmdImpl)
	registrar.Subscribe[testKeyEventHandler](func() (kernel.Lock, kernel.Observe[KeyEvent]) {
		return nil, func(_ kernel.Kernel, event KeyEvent) error {
			p.keyc <- event
			return nil
		}
	})
	return nil
}

func probeCmdImpl() (kernel.Lock, kernel.Execute[probeRequest, probeResponse]) {
	var state kernel.Read[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetRead[*State]()
		}, func(kernel.Kernel, probeRequest) (probeResponse, error) {
			s := state.Get()
			return probeResponse{Pressed: s.Pressed(KeyA), Just: s.JustPressed(KeyA)}, nil
		}
}

// End-to-end: Apply folds a key press into State and publishes KeyEvent; an
// app.UpdateEvent rolls the edge so JustPressed shows up when polled.
func TestInputPluginApplyPollAndEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	keyc := make(chan KeyEvent, 8)
	engine := kernel.New(nil).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(New(), probePlugin{keyc: keyc})
	go engine.Run(ctx)
	<-engine.Ready()
	k := engine.Executioner()
	// Apply a key-down batch.
	k.ExecuteCommand[ApplyCmd](ApplyRequest{Changes: []Change{KeyChange(KeyA, ModShift, true)}})

	// The discrete KeyEvent should have been published.
	select {
	case p := <-keyc:
		if p.Key != KeyA || !p.Down || !p.Mods.Has(ModShift) {
			t.Fatalf("KeyEvent = %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for KeyEvent")
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
	response, err := k.ExecuteCommand[probeCmd](probeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return response
}
