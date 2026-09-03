package anim

import (
	"context"
	"slices"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

type seedCmd kernel.Command[seedRequest, seedResponse]
type seedRequest struct{}
type seedResponse struct{}

type probeCmd kernel.Command[probeRequest, probeResponse]
type probeRequest struct{}
type probeResponse struct {
	Value float32
	State State
	Idle  bool
	Fired []string
}

type probeKey struct{}

type probePlugin struct{}

func (probePlugin) Name() kernel.PluginName { return "test" }

// Name is the anim plugin's, not this fixture's: the probe locks anim.Timelines.
func (probePlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }
func (probePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[seedCmd](seedCmdImpl)
	registrar.HandleCommand[probeCmd](probeCmdImpl)
	return nil
}

// seedCmdImpl queues a cue at the (idle) chain point and a one-second track.
func seedCmdImpl() (kernel.Lock, kernel.Execute[seedRequest, seedResponse]) {
	var timelines kernel.Write[*Timelines]
	return func(access kernel.ResourceAccess) {
			timelines = access.GetWrite[*Timelines]()
		}, func(kernel.Kernel, seedRequest) (seedResponse, error) {
			tl := timelines.Get().Get(probeKey{})
			tl.Cue("hello")
			tl.Add("id", LerpFloat(0, 1), Over(1))
			return seedResponse{}, nil
		}
}

func probeCmdImpl() (kernel.Lock, kernel.Execute[probeRequest, probeResponse]) {
	var timelines kernel.Read[*Timelines]
	return func(access kernel.ResourceAccess) {
			timelines = access.GetRead[*Timelines]()
		}, func(kernel.Kernel, probeRequest) (probeResponse, error) {
			tl := timelines.Get().Lookup(probeKey{})
			_, _, state := tl.Query[Lerp[float32]]("id")
			return probeResponse{
				Value: tl.Value[Lerp[float32]]("id", -1),
				State: state,
				Idle:  tl.Idle(),
				Fired: slices.Collect(tl.Fired[string]()),
			}, nil
		}
}

// End-to-end: the plugin advances the seeded timeline on each app.UpdateEvent,
// fires the cue for exactly one tick, and drops the track once it has passed.
func TestAnimPluginAdvancesOnUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine := kernel.New(nil).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(New(), probePlugin{})
	go engine.Run(ctx)
	<-engine.Ready()
	k := engine.Executioner()

	if _, err := k.ExecuteCommand[seedCmd](seedRequest{}); err != nil {
		t.Fatal(err)
	}
	if response := probe(t, k); response.Value != 0 || response.State != StateActive || response.Idle || len(response.Fired) != 0 {
		t.Fatalf("before any tick: %+v, want value 0 active, not idle, no cues", response)
	}

	k.PublishEvent(app.UpdateEvent{Dt: 0.5}).Wait()
	if response := probe(t, k); response.Value != 0.5 || response.State != StateActive || !slices.Equal(response.Fired, []string{"hello"}) {
		t.Fatalf("after 0.5s: %+v, want value 0.5 active with [hello]", response)
	}

	k.PublishEvent(app.UpdateEvent{Dt: 0.5}).Wait()
	if response := probe(t, k); response.Value != 1 || response.State != StateActive || len(response.Fired) != 0 {
		t.Fatalf("after 1s: %+v, want value 1 active with no cues", response)
	}

	k.PublishEvent(app.UpdateEvent{Dt: 0.5}).Wait()
	if response := probe(t, k); response.Value != -1 || response.State != StateNotFound || !response.Idle {
		t.Fatalf("after 1.5s: %+v, want fallback, not found, idle", response)
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
