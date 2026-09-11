package wgpu

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
	"github.com/dvoyni/cog/storage"
	"github.com/dvoyni/cog/ui"
)

// The pairing recipe is the umbrella's headline promise and the thing #259
// found broken: three snapshots armed together described two ticks, eleven
// times out of eleven, against a real window. The properties worth pinning
// here are therefore the ones a fixture cannot fake - the real tick source,
// the real capability bodies of three different packages, and a frame loop
// running underneath them that is free to consume a batch mid-recipe.
//
// This is the closest the repository gets to the live test. What it does not
// have is a window, a GPU and three HTTP connections; what it does have is
// every seam between them.

// pairingRounds is how many times each recipe is run. The live failure was a
// race against the frame clock rather than a fixed limit, so one pass proves
// nothing: at 10 ms a frame this is a few hundred chances for an arm to lose
// the race it used to lose.
const pairingRounds = 25

// pairingPlugin registers the driver's real time-control handler against an
// engine the driver itself cannot join, because wgpu's own Register needs a
// window and the tick source does not. It is the same trick tickTestPlugin
// uses, with the snapshot-owning plugins beside it.
type pairingPlugin struct{ rig *pairingRig }

func (p *pairingPlugin) Name() kernel.PluginName { return "pairingtest" }

func (p *pairingPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.Name, canvas.Name, ui.Name}
}

func (p *pairingPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[app.TimeCmd](p.rig.plugin.timeCmdImpl)
	return nil
}

// pairingRig is a whole engine - storage, input, gfx, canvas, ui and the
// driver's tick source - with a frame loop of its own, driven the way the
// host drives it: onUpdate on one goroutine, a render event behind it.
type pairingRig struct {
	t       *testing.T
	plugin  *Plugin
	k       kernel.Executioner
	caps    map[string]mcp.Capability
	backend *pairingBackend

	stopLoop func()
}

func newPairingRig(t *testing.T) *pairingRig {
	t.Helper()
	rig := &pairingRig{
		t: t, plugin: &Plugin{config: tickTestConfig()},
		caps: map[string]mcp.Capability{}, backend: newPairingBackend(),
	}
	gfxPlugin, canvasPlugin, uiPlugin := gfx.New(), canvas.New(), ui.New()

	ctx, cancel := context.WithCancel(context.Background())
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("cog-pairing-test"),
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(
		storage.New(), input.New(), gfxPlugin, canvasPlugin, uiPlugin,
		&pairingPlugin{rig: rig},
	)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		engine.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Error("engine did not shut down")
		}
	})
	<-engine.Ready()
	rig.k = engine.Executioner()

	// The capabilities are collected exactly as the broker collects them, so
	// what the test calls is what an agent calls.
	for _, provider := range []mcp.Provider{gfxPlugin, canvasPlugin, uiPlugin, rig.plugin} {
		for _, capability := range provider.Capabilities() {
			if err := capability.Err(); err != nil {
				t.Fatalf("capability %s: %v", capability.Name(), err)
			}
			rig.caps[string(provider.Name())+"_"+capability.Name()] = capability
		}
	}

	rig.k.ExecuteCommand[gfx.SetBackendCmd](gfx.SetBackendRequest{Backend: rig.backend})
	rig.k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	return rig
}

// run starts the frame loop: onUpdate on a goroutine of its own, then the
// render event the driver publishes from its draw callback. This is what the
// join window was racing, and what a hand-stepped test can never reproduce.
func (r *pairingRig) run() {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-done:
				return
			default:
			}
			r.plugin.frameSeq.Add(1)
			r.plugin.onUpdate(r.k, 0)
			r.k.PublishEvent(app.RenderEvent{}).Wait()
			time.Sleep(time.Millisecond)
		}
	}()
	r.stopLoop = func() {
		close(done)
		<-stopped
	}
}

func (r *pairingRig) stop() {
	if r.stopLoop != nil {
		r.stopLoop()
		r.stopLoop = nil
	}
}

// time calls wgpu_time the way an agent does, through the capability rather
// than the command, so the agent-facing refusals are exercised too.
func (r *pairingRig) time(request TimeRequest) TimeResponse {
	r.t.Helper()
	answer, err := r.caps["wgpu_time"].Invoke(r.k, &request)
	if err != nil {
		r.t.Fatalf("wgpu_time %s: %v", request.Action, err)
	}
	return answer.(TimeResponse)
}

// invoke calls one capability on its own goroutine and hands back the wait,
// which is what three parallel tool calls look like from the engine's side.
func (r *pairingRig) invoke(name string, request any) <-chan pairingAnswer {
	done := make(chan pairingAnswer, 1)
	go func() {
		response, err := r.caps[name].Invoke(r.k, request)
		done <- pairingAnswer{name: name, response: response, err: err}
	}()
	return done
}

type pairingAnswer struct {
	name     string
	response any
	err      error
}

// snapshotView pulls out the block every snapshot response embeds. The type
// switch is the point: one view, three capabilities, so an agent reads one
// tick number whichever tool answered.
func (a pairingAnswer) snapshotView(t *testing.T) gfx.SnapshotView {
	t.Helper()
	switch response := a.response.(type) {
	case gfx.FrameResponse:
		return response.SnapshotView
	case canvas.DrawsResponse:
		return response.SnapshotView
	case ui.LayoutResponse:
		return response.SnapshotView
	}
	t.Fatalf("%s answered with %T, which carries no snapshot view", a.name, a.response)
	return gfx.SnapshotView{}
}

// snapshotArms is the recipe's parallel half: the three capabilities that
// each need a tick, armed together.
func (r *pairingRig) snapshotArms() []<-chan pairingAnswer {
	return []<-chan pairingAnswer{
		r.invoke("gfx_frame", &gfx.FrameRequest{}),
		r.invoke("canvas_draws", &canvas.DrawsRequest{}),
		r.invoke("ui_layout", &ui.LayoutRequest{}),
	}
}

// collect waits for every arm and reports the tick each said it describes.
func (r *pairingRig) collect(arms []<-chan pairingAnswer) []gfx.SnapshotView {
	r.t.Helper()
	views := make([]gfx.SnapshotView, 0, len(arms))
	for _, arm := range arms {
		select {
		case answer := <-arm:
			if answer.err != nil {
				r.t.Fatalf("%s: %v", answer.name, answer.err)
			}
			views = append(views, answer.snapshotView(r.t))
		case <-time.After(15 * time.Second):
			r.t.Fatal("an arm never answered")
		}
	}
	return views
}

// waitSharing blocks until the arms are all waiting on one step, which is the
// moment the recipe's release belongs at. An agent has no such signal and
// relies on the hold's own deadline instead; a test that released blind would
// be asserting its own timing rather than the mechanism.
func (r *pairingRig) waitSharing(callers int64) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.plugin.ticks.mu.Lock()
		batch := r.plugin.ticks.batch
		r.plugin.ticks.mu.Unlock()
		if batch != nil && batch.sharing.Load() == callers {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("no step is shared by %d arms", callers)
		}
		time.Sleep(time.Millisecond)
	}
}

// Three snapshots armed under one hold describe one tick, and say so: every
// response carries the same tick number, which is the part an agent can check
// for itself. Repeated against a frame loop that is free to take the batch
// between any two of them.
func TestPairing_ThreeSnapshotsUnderAHoldDescribeOneTick(t *testing.T) {
	rig := newPairingRig(t)
	rig.run()
	defer rig.stop()

	rig.time(TimeRequest{Action: "pause"})
	seen := map[int64]bool{}
	for round := range pairingRounds {
		rig.time(TimeRequest{Action: "hold", Ms: 5000})
		arms := rig.snapshotArms()
		rig.waitSharing(3)
		rig.time(TimeRequest{Action: "release"})

		views := rig.collect(arms)
		tick, stepped := views[0].Tick, 0
		for _, view := range views {
			if view.Tick != tick {
				t.Fatalf("round %d: the three snapshots describe ticks %d and %d, want one tick",
					round, tick, view.Tick)
			}
			if view.Stepped {
				stepped++
			}
		}
		if tick == 0 {
			t.Fatalf("round %d: the snapshots name no tick at all", round)
		}
		if stepped != 3 {
			t.Fatalf("round %d: %d of three snapshots reported a step, want all three", round, stepped)
		}
		if seen[tick] {
			t.Fatalf("round %d: tick %d was already described by an earlier round", round, tick)
		}
		seen[tick] = true
	}
	if len(seen) != pairingRounds {
		t.Errorf("%d rounds described %d distinct ticks, want one each", pairingRounds, len(seen))
	}
}

// Exactly one of the arms raises the step and the rest join it, whichever
// order they arrive in. Joined on its own was never evidence - two snapshots
// both reporting a step may be a tick apart - but with the tick number
// agreeing it says which arm paid for the tick they share.
func TestPairing_OneArmRaisesTheStepAndTheRestJoinIt(t *testing.T) {
	rig := newPairingRig(t)
	rig.run()
	defer rig.stop()

	rig.time(TimeRequest{Action: "pause"})
	for round := range pairingRounds {
		rig.time(TimeRequest{Action: "hold", Ms: 5000})
		arms := rig.snapshotArms()
		rig.waitSharing(3)
		rig.time(TimeRequest{Action: "release"})

		joined := 0
		for _, view := range rig.collect(arms) {
			if view.Joined {
				joined++
			}
		}
		if joined != 2 {
			t.Fatalf("round %d: %d of three arms joined, want the two that did not raise the step",
				round, joined)
		}
	}
}

// The recipe the specs prescribe, whole and in order: pause, hold, the three
// snapshots in parallel, release, and a capture last. All four describe one
// tick - the three by naming it, the capture by costing none, so the frozen
// frame it photographs is the one that step produced.
func TestPairing_TheWholeRecipeDescribesOneTick(t *testing.T) {
	rig := newPairingRig(t)
	rig.run()
	defer rig.stop()

	directory := t.TempDir()
	rig.time(TimeRequest{Action: "pause"})
	for round := range pairingRounds {
		rig.time(TimeRequest{Action: "hold", Ms: 5000})
		arms := rig.snapshotArms()
		rig.waitSharing(3)
		rig.time(TimeRequest{Action: "release"})

		views := rig.collect(arms)
		tick := views[0].Tick
		for _, view := range views {
			if view.Tick != tick {
				t.Fatalf("round %d: the three snapshots describe ticks %d and %d, want one tick",
					round, tick, view.Tick)
			}
		}

		before := rig.time(TimeRequest{Action: "status"})
		if before.Tick != tick {
			t.Fatalf("round %d: the engine is at tick %d and the snapshots describe %d",
				round, before.Tick, tick)
		}
		answer := <-rig.invoke("gfx_capture", &gfx.CaptureRequest{
			Path: filepath.Join(directory, fmt.Sprintf("round-%02d.png", round)),
		})
		if answer.err != nil {
			t.Fatalf("round %d: gfx_capture: %v", round, answer.err)
		}
		after := rig.time(TimeRequest{Action: "status"})
		if after.Tick != tick {
			t.Fatalf("round %d: the capture advanced the engine from tick %d to %d, so it "+
				"photographed a different moment", round, tick, after.Tick)
		}
		if after.Advanced != before.Advanced {
			t.Fatalf("round %d: the capture cost %d ticks, want none",
				round, after.Advanced-before.Advanced)
		}
	}
}

// Without a hold the arms are back to racing the frame clock, and the tick
// numbers are what make the outcome legible either way. Nothing here asserts
// that a split happens - it is a race, and on a fast enough machine three
// arms may still land together - only that whatever happened is readable from
// the responses alone, which is what an agent has.
func TestPairing_ASplitIsVisibleInTheResponses(t *testing.T) {
	rig := newPairingRig(t)
	rig.run()
	defer rig.stop()

	rig.time(TimeRequest{Action: "pause"})
	arms := rig.snapshotArms()
	views := rig.collect(arms)

	ticks := map[int64]int{}
	for _, view := range views {
		if view.Tick == 0 {
			t.Fatal("a snapshot named no tick, so an agent cannot tell a pairing from a split")
		}
		ticks[view.Tick]++
	}
	t.Logf("three arms with no hold described %d distinct tick(s)", len(ticks))
}

// A hold that runs out publishes the step it was keeping open rather than
// stranding the arms waiting on it, and the next answer from wgpu_time names
// the expiry instead of leaving it to be inferred from a split.
func TestPairing_AnExpiredHoldReleasesTheArmsAndIsReported(t *testing.T) {
	rig := newPairingRig(t)
	rig.run()
	defer rig.stop()

	rig.time(TimeRequest{Action: "pause"})
	rig.time(TimeRequest{Action: "hold", Ms: 100})
	arms := rig.snapshotArms()

	views := rig.collect(arms)
	tick := views[0].Tick
	for _, view := range views {
		if view.Tick != tick {
			t.Errorf("an expired hold split the arms across ticks %d and %d", tick, view.Tick)
		}
	}
	status := rig.time(TimeRequest{Action: "status"})
	if status.Held {
		t.Error("the hold is still standing after its deadline")
	}
	if !status.HoldExpired {
		t.Error("status does not report that the hold ran out instead of being released")
	}
}

// A snapshot's own deadline is not spent on a hold somebody deliberately took
// out: a hold longer than the two seconds that name a stalled engine still
// ends in a snapshot rather than in a refusal.
func TestPairing_AHoldIsNotChargedAgainstASnapshotDeadline(t *testing.T) {
	rig := newPairingRig(t)
	rig.run()
	defer rig.stop()

	rig.time(TimeRequest{Action: "pause"})
	rig.time(TimeRequest{Action: "hold", Ms: 3000})
	arms := rig.snapshotArms()
	rig.waitSharing(3)

	time.Sleep(2500 * time.Millisecond)
	rig.time(TimeRequest{Action: "release"})

	views := rig.collect(arms)
	for _, view := range views {
		if view.Tick != views[0].Tick {
			t.Fatalf("a long hold split the arms across ticks %d and %d", views[0].Tick, view.Tick)
		}
	}
}

// pairingBackend is the least backend gfx will work against: every handle is
// a counter, every pass is dropped, and a capture hands back a two-by-two
// picture so the capture path runs end to end without a GPU.
type pairingBackend struct {
	mu      sync.Mutex
	next    uint32
	pending *gfx.GpuCapture
	ready   *gfx.GpuCapture
}

func newPairingBackend() *pairingBackend { return &pairingBackend{} }

func (b *pairingBackend) id() gfx.ResourceID {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	return gfx.ResourceID(b.next)
}

func (b *pairingBackend) NewTexture() gfx.TextureID { return gfx.TextureID(b.id()) }
func (b *pairingBackend) NewBuffer() gfx.BufferID   { return gfx.BufferID(b.id()) }

func (b *pairingBackend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	return gfx.SamplerID(b.id()), nil
}
func (b *pairingBackend) FreeSampler(gfx.SamplerID) {}

func (b *pairingBackend) NewShader(gfx.ShaderDesc) (gfx.ShaderID, error) {
	return gfx.ShaderID(b.id()), nil
}
func (b *pairingBackend) FreeShader(gfx.ShaderID)                    {}
func (b *pairingBackend) ShaderLayout(gfx.ShaderID) gfx.ShaderLayout { return gfx.ShaderLayout{} }
func (b *pairingBackend) FreePipeline(gfx.PipelineID)                {}
func (b *pairingBackend) Limits() gfx.Limits                         { return gfx.DefaultLimits }
func (b *pairingBackend) TransitionTextures([]gfx.TextureTransition) {}
func (b *pairingBackend) EndPass(gfx.RenderPass)                     {}
func (b *pairingBackend) Present()                                   {}
func (b *pairingBackend) BeginPass(gfx.GpuPassDesc) gfx.RenderPass   { return nil }

func (b *pairingBackend) NewPipeline(gfx.PipelineDesc) (gfx.PipelineID, error) {
	return gfx.PipelineID(b.id()), nil
}

func (b *pairingBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return gfx.TextureViewID(1), 1600, 1200
}

func (b *pairingBackend) TextureView(gfx.TextureID, int, int) gfx.TextureViewID {
	return gfx.TextureViewID(b.id())
}

// Execute promotes what the previous frame staged and then replays this
// frame, so that a capture op reaches Capture below. The promotion comes
// first because the real backend's readback has one frame of latency - the
// map resolves on the submit after the copy - and gfx counts on it: it drains
// before it marks the copy encoded, so a capture handed back inside its own
// frame is dropped as one nobody is waiting for.
func (b *pairingBackend) Execute(queue *gfx.GpuQueue) {
	b.mu.Lock()
	b.ready, b.pending = b.pending, nil
	b.mu.Unlock()
	queue.ReplayPasses(b)
}

func (b *pairingBackend) Capture(gfx.GpuCaptureDesc) {
	picture := gfx.GpuCapture{
		Width: 2, Height: 2, Format: gfx.FormatRGBA8, BytesPerRow: 256,
		Pixels: make([]byte, 256*2),
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = &picture
}

func (b *pairingBackend) TakeCapture() (gfx.GpuCapture, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ready == nil {
		return gfx.GpuCapture{}, false
	}
	done := *b.ready
	b.ready = nil
	return done, true
}
