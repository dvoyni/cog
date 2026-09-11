package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/mcp"
	"github.com/dvoyni/cog/storage"
)

// canvas_draws answers "nothing is on screen; was it even recorded, and on
// which layer", so the properties worth pinning are the ones that would make
// the answer a lie: a snapshot of the tick before the request, ui's recording
// missing from a queue it shares, an index that stops addressing what it named
// once a filter is on, and a paused engine that either hangs or silently never
// steps.

// snapshotFixture is the engine around canvas that a snapshot test drives by
// hand. It plays three parts a real game has and a canvas test otherwise does
// not: the app's own recording plugin, ui - which records into the same queue
// from the earlier phase, which is the whole reason canvas_draws sees what ui
// emitted - and the tick source, which answers whether the engine is paused
// and publishes the tick a step owes.
type snapshotFixture struct {
	mu sync.Mutex
	// record and recordUI are what the two recorders put in the queue. They are
	// separate subscriptions, ordered as the app's and ui's are, so a test
	// asserting that both reach the snapshot is asserting about the ordering
	// rather than about one callback writing twice.
	record, recordUI func(*OpQueue)
	step             func(kernel.Kernel, app.TimeRequest) (app.TimeResponse, error)
	requests         []app.TimeRequest

	paused atomic.Bool
}

// appRecordHandler stands in for the app's own recording plugin, and
// uiRecordHandler for ui, which subscribes exactly here: the default phase,
// ordered before canvas's flush (ui/plugin.go).
type appRecordHandler kernel.Subscription[app.UpdateEvent]
type uiRecordHandler kernel.Subscription[app.UpdateEvent]

func (*snapshotFixture) Name() kernel.PluginName { return "canvas-snapshot-test" }

// Dependencies names canvas, whose OpQueue both recorders lock, and gfx, whose
// resource queue the render-target probe does.
func (*snapshotFixture) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{Name, gfx.Name, storage.Name}
}

func (f *snapshotFixture) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[appRecordHandler](f.recordOnUpdate)
	registrar.Subscribe[uiRecordHandler](f.recordUIOnUpdate).Before[UpdateEventHandler]()
	registrar.HandleCommand[app.TimeCmd](f.timeCmdImpl)
	registrar.HandleCommand[gfxResourceProbeCmd](gfxResourceProbeCmdImpl)
	return nil
}

func (f *snapshotFixture) recordOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*OpQueue]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			f.mu.Lock()
			record := f.record
			f.mu.Unlock()
			if record != nil {
				record(queue.Get())
			}
			return nil
		}
}

func (f *snapshotFixture) recordUIOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*OpQueue]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			f.mu.Lock()
			record := f.recordUI
			f.mu.Unlock()
			if record != nil {
				record(queue.Get())
			}
			return nil
		}
}

func (f *snapshotFixture) timeCmdImpl() (kernel.Lock, kernel.Execute[app.TimeRequest, app.TimeResponse]) {
	return nil, func(k kernel.Kernel, request app.TimeRequest) (app.TimeResponse, error) {
		f.mu.Lock()
		f.requests = append(f.requests, request)
		step := f.step
		f.mu.Unlock()
		if request.Action == app.TimeStep && step != nil {
			return step(k, request)
		}
		return app.TimeResponse{Paused: f.paused.Load()}, nil
	}
}

func (f *snapshotFixture) on(record func(*OpQueue))   { f.set(&f.record, record) }
func (f *snapshotFixture) onUI(record func(*OpQueue)) { f.set(&f.recordUI, record) }

func (f *snapshotFixture) set(slot *func(*OpQueue), record func(*OpQueue)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*slot = record
}

func (f *snapshotFixture) onStep(step func(kernel.Kernel, app.TimeRequest) (app.TimeResponse, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.step = step
}

func (f *snapshotFixture) asked() []app.TimeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// drawsRig is a canvas engine a snapshot test drives by hand: one tick is an
// app.UpdateEvent, and the viewport is set up so the three coordinate sizes
// are three different numbers.
type drawsRig struct {
	t       *testing.T
	k       kernel.Executioner
	fixture *snapshotFixture
}

func newDrawsRig(t *testing.T) *drawsRig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	fixture := &snapshotFixture{}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("canvas-draws-test").
			WithReadFS("test", 10, fstest.MapFS{}),
		Name: DefaultConfig(),
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(storage.New(), gfx.New(), New(), fixture)
	stopped := make(chan struct{})
	go func() { engine.Run(ctx); close(stopped) }()
	<-engine.Ready()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})

	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	k.ExecuteCommand[gfx.SetBackendCmd](gfx.SetBackendRequest{Backend: &testBackend{}})
	// A fixed-width policy is what makes the logical viewport differ from the
	// window, so a response reporting only two of the three sizes is visible.
	k.ExecuteCommand[app.SetDesiredViewportCmd](app.SetDesiredViewportRequest{
		Mode: app.ViewportFixedWidth, Size: 400,
	})
	k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	return &drawsRig{t: t, k: k, fixture: fixture}
}

func (r *drawsRig) tick() {
	r.t.Helper()
	r.k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
}

// runDraws calls the capability body on its own goroutine and drives ticks
// until it answers, which is what an agent's call looks like from the engine's
// side. What the recorders put in each tick is whatever the test installed.
func (r *drawsRig) runDraws(request DrawsRequest) (DrawsResponse, error) {
	r.t.Helper()
	type answer struct {
		response DrawsResponse
		err      error
	}
	done := make(chan answer, 1)
	go func() {
		response, err := drawsSnapshot(r.k, request)
		done <- answer{response, err}
	}()
	expiry := time.After(10 * time.Second)
	for {
		select {
		case got := <-done:
			return got.response, got.err
		case <-expiry:
			r.t.Fatal("the snapshot never answered")
		default:
		}
		r.tick()
		time.Sleep(time.Millisecond)
	}
}

// aSpriteAndTriangles is one recorder's frame: a sprite and a text on the
// lower layer, a triangle list on the upper one.
func aSpriteAndTriangles(queue *OpQueue) {
	queue.Sprite(1, "images/hero.png", SpriteTransform{
		Position: m.Vec2{X: 10, Y: 20}, Size: m.Vec2{X: 32, Y: 48},
		Filter: gfx.FilterNearest,
	}, nil, gfx.ColorParam(TintSlot, m.Color{R: 1, A: 1}))
	queue.Text(1, "fonts/body.ttf", "score", TextDraw{
		Position: m.Vec2{X: 4, Y: 6}, Size: 12, Color: m.Color{G: 1, A: 1}, Align: AlignCenter,
	})
	queue.DrawTriangles(3, triangleFan(), nil)
}

// triangleFan is six built-in vertices whose positions span a known box.
func triangleFan() []Vertex {
	return []Vertex{
		{Position: m.Vec2{X: -5, Y: -5}, Color: m.Color{R: 1, A: 1}},
		{Position: m.Vec2{X: 15, Y: -5}, Color: m.Color{G: 1, A: 1}},
		{Position: m.Vec2{X: 15, Y: 25}, Color: m.Color{B: 1, A: 1}},
		{Position: m.Vec2{X: -5, Y: -5}, UV: m.Vec2{X: 1}},
		{Position: m.Vec2{X: 15, Y: 25}, UV: m.Vec2{Y: 1}},
		{Position: m.Vec2{X: -5, Y: 25}, UV: m.Vec2{X: 1, Y: 1}},
	}
}

func TestADrawsSnapshotCarriesTheAppsRecordingAndTheUIsFromOneTick(t *testing.T) {
	rig := newDrawsRig(t)
	rig.fixture.on(aSpriteAndTriangles)
	// ui records into the same queue during its own processing, one phase
	// earlier. That is what makes canvas_draws and ui_layout complementary
	// rather than redundant, so a snapshot missing it is answering the wrong
	// question.
	rig.fixture.onUI(func(queue *OpQueue) {
		queue.FillRect(2, m.Rect{X: 1, Y: 2, Width: 3, Height: 4},
			ShapeDraw{Color: m.Color{B: 1, A: 1}})
	})

	response, err := rig.runDraws(DrawsRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if response.OpCount != 4 || len(response.Ops) != 4 {
		t.Fatalf("ops = %d of %d, want the app's three and ui's one",
			len(response.Ops), response.OpCount)
	}
	// Flush order is layers ascending, then recording order within a layer, so
	// ui's layer-2 fill sits between the app's layer-1 pair and its layer-3
	// triangles however late ui ran.
	kinds := make([]string, 0, len(response.Ops))
	for i, op := range response.Ops {
		kinds = append(kinds, op.Kind)
		if op.Index != i {
			t.Errorf("op %d carries index %d, want its position in record order", i, op.Index)
		}
	}
	if !slices.Equal(kinds, []string{"sprite", "text", "sprite", "triangles"}) {
		t.Fatalf("kinds = %v, want the app's sprite and text, ui's fill, then the triangles", kinds)
	}
	if response.Ops[2].Layer != 2 {
		t.Errorf("ui's op is on layer %d, want the layer it recorded into", response.Ops[2].Layer)
	}
	sprite := response.Ops[0]
	if sprite.Path != "images/hero.png" || sprite.Transform == nil {
		t.Fatalf("sprite = %+v, want its path and transform", sprite)
	}
	if sprite.Transform.Filter != "nearest" {
		t.Errorf("filter = %q, want the enum named rather than numbered", sprite.Transform.Filter)
	}
	if len(sprite.Params) != 1 || sprite.Params[0].Name != TintSlot {
		t.Errorf("params = %+v, want the recorded tint", sprite.Params)
	}
	text := response.Ops[1]
	if text.Text != "score" || text.Draw == nil || text.Draw.Align != "center" {
		t.Fatalf("text = %+v, want its string and its alignment named", text)
	}
	// The three coordinate sizes, including the logical one no other response
	// reports and the one that breaks the click flow silently when missing.
	if response.PixelWidth != 1600 || response.PixelHeight != 1200 {
		t.Errorf("pixels = %dx%d, want 1600x1200", response.PixelWidth, response.PixelHeight)
	}
	if response.WindowWidth != 800 || response.WindowHeight != 600 {
		t.Errorf("window = %vx%v, want 800x600", response.WindowWidth, response.WindowHeight)
	}
	if response.ViewportWidth != 400 || response.ViewportHeight != 300 {
		t.Errorf("viewport = %vx%v, want the logical size the sizing policy resolved to",
			response.ViewportWidth, response.ViewportHeight)
	}
	if response.Stepped {
		t.Error("a running engine was reported as having been stepped")
	}
}

func TestADrawsSnapshotReportsEachLayersWindowTargetAndClear(t *testing.T) {
	rig := newDrawsRig(t)
	var target gfx.TargetDescr
	var texture gfx.TextureDescr
	rig.fixture.on(func(queue *OpQueue) {
		queue.SetLayerTransform(1, m.Rect{X: -8, Y: -6, Width: 16, Height: 12}, AspectOverlap)
		queue.Clear(1, m.Color{R: 0.25, A: 1})
		queue.Sprite(1, "images/hero.png", SpriteTransform{Size: m.Vec2{X: 1, Y: 1}}, nil)
		queue.SetLayerTarget(2, target)
		queue.Sprite(2, "images/hero.png", SpriteTransform{Size: m.Vec2{X: 1, Y: 1}}, nil)
	})
	// The target is a gfx handle the caller allocated: canvas mints nothing
	// here and passes it through untouched, so reporting where a layer draws
	// means reading it back out of the descriptor.
	withGfxResources(t, rig.k, func(resources *gfx.ResourceQueue) {
		texture = resources.AllocateRenderTarget(64, 32, 1, gfx.FormatRGBA8)
		target = gfx.TextureTarget(texture, 0, 0)
	})

	response, err := rig.runDraws(DrawsRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(response.Layers) != 2 || response.LayerCount != 2 {
		t.Fatalf("layers = %+v, want the two the frame touched", response.Layers)
	}
	world, offscreen := response.Layers[0], response.Layers[1]
	if world.Layer != 1 || offscreen.Layer != 2 {
		t.Fatalf("layers = %d then %d, want them ascending", world.Layer, offscreen.Layer)
	}
	// Without the window an op's coordinates mean nothing: they are in the
	// layer's own world space, not the viewport's.
	if world.Window == nil || *world.Window != (RectView{X: -8, Y: -6, Width: 16, Height: 12}) {
		t.Errorf("window = %+v, want the rectangle SetLayerTransform was given", world.Window)
	}
	if world.Aspect != "overlap" {
		t.Errorf("aspect = %q, want the mode named rather than numbered", world.Aspect)
	}
	if world.Target != "screen" || world.TargetTexture != 0 {
		t.Errorf("a layer that named no target reports %q, want screen", world.Target)
	}
	if !equalFloat32s(world.Clear, []float32{0.25, 0, 0, 1}) {
		t.Errorf("clear = %v, want the colour the layer clears to", world.Clear)
	}
	if world.Ops != 1 {
		t.Errorf("layer 1 recorded %d ops, want the one it did", world.Ops)
	}
	if offscreen.Target != "texture" || offscreen.TargetTexture != texture.ID() {
		t.Errorf("target = %q/%d, want the texture the layer draws into",
			offscreen.Target, offscreen.TargetTexture)
	}
	if offscreen.TargetWidth != 64 || offscreen.TargetHeight != 32 {
		t.Errorf("target size = %dx%d, want 64x32", offscreen.TargetWidth, offscreen.TargetHeight)
	}
	if offscreen.Clear != nil {
		t.Errorf("clear = %v, want none on a layer that never cleared", offscreen.Clear)
	}
}

func TestTriangleVerticesAreSummarisedUntilAnOpIsNamed(t *testing.T) {
	rig := newDrawsRig(t)
	rig.fixture.on(aSpriteAndTriangles)

	summary, err := rig.runDraws(DrawsRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	triangles := summary.Ops[len(summary.Ops)-1]
	if triangles.Kind != "triangles" {
		t.Fatalf("last op = %q, want the triangle list", triangles.Kind)
	}
	if triangles.Vertices != nil {
		t.Errorf("vertices came back unasked: %d of them would swamp a busy frame",
			len(triangles.Vertices))
	}
	if triangles.VertexCount != 6 {
		t.Errorf("vertexCount = %d, want the six recorded", triangles.VertexCount)
	}
	want := RectView{X: -5, Y: -5, Width: 20, Height: 30}
	if triangles.Bounds == nil || *triangles.Bounds != want {
		t.Errorf("bounds = %+v, want %+v", triangles.Bounds, want)
	}

	// The drill-down after a filter that removed every earlier op is the case
	// source indices exist for: the index the summary reported is still the
	// address, and a position in the emitted array would now name nothing.
	index := triangles.Index
	if index != 2 {
		t.Fatalf("the triangle op is at index %d, want its position in record order", index)
	}
	full, err := rig.runDraws(DrawsRequest{Kinds: []string{"triangles"}, Vertices: []int{index}})
	if err != nil {
		t.Fatalf("drill-down: %v", err)
	}
	if len(full.Ops) != 1 || full.Ops[0].Index != index {
		t.Fatalf("filtered ops = %+v, want only the triangle list, still at %d", full.Ops, index)
	}
	if len(full.Ops[0].Vertices) != 6 {
		t.Fatalf("expanded vertices = %d, want the six the op recorded",
			len(full.Ops[0].Vertices))
	}
	first := full.Ops[0].Vertices[0]
	if !equalFloat32s(first.Position, []float32{-5, -5}) ||
		!equalFloat32s(first.Color, []float32{1, 0, 0, 1}) {
		t.Errorf("first vertex = %+v, want the one recorded", first)
	}
}

func TestAFilteredDrawsSnapshotKeepsSourceIndicesAndNamesWhatItDropped(t *testing.T) {
	rig := newDrawsRig(t)
	rig.fixture.on(aSpriteAndTriangles)

	whole, err := rig.runDraws(DrawsRequest{})
	if err != nil {
		t.Fatalf("unfiltered snapshot: %v", err)
	}
	upper := 3
	filtered, err := rig.runDraws(DrawsRequest{FromLayer: &upper})
	if err != nil {
		t.Fatalf("filtered snapshot: %v", err)
	}
	if len(filtered.Ops) != 1 || filtered.Ops[0].Kind != "triangles" {
		t.Fatalf("filtered ops = %+v, want only what layer 3 recorded", filtered.Ops)
	}
	// The index is the address: it is a position in canvas flush order, never
	// a position in the emitted array, so an index read off a filtered
	// response still names the same op in an unfiltered one.
	last := whole.Ops[len(whole.Ops)-1]
	if filtered.Ops[0].Index != last.Index {
		t.Errorf("filtered index = %d, unfiltered = %d; filtering moved the address",
			filtered.Ops[0].Index, last.Index)
	}
	if filtered.OmittedOps != 2 || filtered.OmittedLayers != 1 {
		t.Errorf("omitted = %d ops / %d layers, want the two and the one the filter dropped",
			filtered.OmittedOps, filtered.OmittedLayers)
	}
	// And the totals still describe the whole frame, so the agent can see how
	// much of it the filter hid.
	if filtered.OpCount != whole.OpCount || filtered.LayerCount != whole.LayerCount {
		t.Errorf("filtered totals = %d ops / %d layers, want the whole frame's %d / %d",
			filtered.OpCount, filtered.LayerCount, whole.OpCount, whole.LayerCount)
	}
	if filtered.FromLayer == nil || *filtered.FromLayer != upper {
		t.Errorf("the response does not echo the layer filter it was armed with: %+v",
			filtered.FromLayer)
	}

	byKind, err := rig.runDraws(DrawsRequest{Kinds: []string{"text"}})
	if err != nil {
		t.Fatalf("kind-filtered snapshot: %v", err)
	}
	if len(byKind.Ops) != 1 || byKind.Ops[0].Kind != "text" || byKind.Ops[0].Index != 1 {
		t.Fatalf("kind-filtered ops = %+v, want the text op at its own index", byKind.Ops)
	}
	// A kind filter drops ops, never layers: the world window an op's
	// coordinates are in is not something a filter may hide.
	if len(byKind.Layers) != len(whole.Layers) || byKind.OmittedLayers != 0 {
		t.Errorf("layers = %d dropping %d, want every layer kept",
			len(byKind.Layers), byKind.OmittedLayers)
	}
	if !slices.Equal(byKind.Kinds, []string{"text"}) {
		t.Errorf("kinds = %v, want the filter echoed in the words it was given in", byKind.Kinds)
	}
}

func TestADrawsSnapshotBindsToATickThatBeganAfterTheRequest(t *testing.T) {
	rig := newDrawsRig(t)
	// The recorder holds the tick open after recording, so the arm lands
	// inside a tick that has already recorded - the case a between-ticks arm
	// cannot produce and the one a naive implementation gets wrong.
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	rig.fixture.on(func(queue *OpQueue) {
		queue.Text(1, "fonts/body.ttf", "before", TextDraw{Size: 10})
		entered <- struct{}{}
		<-release
	})

	ticked := make(chan struct{})
	go func() {
		rig.tick()
		close(ticked)
	}()
	<-entered
	armed, err := rig.k.ExecuteCommand[ArmDrawsCmd](ArmDrawsRequest{})
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	close(release)
	<-ticked

	select {
	case snapshot := <-armed.Done:
		t.Fatalf("the snapshot described the tick it was armed inside: %+v", snapshot.Draws.Ops)
	default:
	}

	rig.fixture.on(func(queue *OpQueue) {
		queue.Text(1, "fonts/body.ttf", "after", TextDraw{Size: 10})
	})
	rig.tick()

	select {
	case snapshot := <-armed.Done:
		if snapshot.Err != nil {
			t.Fatalf("snapshot failed: %v", snapshot.Err)
		}
		if len(snapshot.Draws.Ops) != 1 || snapshot.Draws.Ops[0].Text != "after" {
			t.Fatalf("the snapshot describes %+v, want the op recorded after the request",
				snapshot.Draws.Ops)
		}
	default:
		t.Fatal("the tick after the request produced no snapshot")
	}
}

func TestASecondDrawsSnapshotIsRefusedInWordsWhileOneIsInFlight(t *testing.T) {
	rig := newDrawsRig(t)
	if _, err := rig.k.ExecuteCommand[ArmDrawsCmd](ArmDrawsRequest{}); err != nil {
		t.Fatalf("first arm: %v", err)
	}

	_, err := drawsSnapshot(rig.k, DrawsRequest{})
	var refusal mcp.Unavailable
	if !errors.As(err, &refusal) {
		t.Fatalf("a second snapshot answered %v, want words an agent can act on", err)
	}
	if !strings.Contains(refusal.Reason, "already in flight") {
		t.Errorf("reason = %q, want it to say one is already in flight", refusal.Reason)
	}

	// A capture and the other packages' snapshots are separate slots. Refusing
	// across kinds would destroy the one thing arming them together is for.
	if _, err := rig.k.ExecuteCommand[gfx.ArmFrameCmd](gfx.ArmFrameRequest{}); err != nil {
		t.Fatalf("a frame snapshot was refused while a draw snapshot was in flight: %v", err)
	}
	if _, err := rig.k.ExecuteCommand[gfx.ArmCaptureCmd](gfx.ArmCaptureRequest{
		Target: gfx.GpuCaptureDesc{Screen: true},
	}); err != nil {
		t.Fatalf("a capture was refused while a draw snapshot was in flight: %v", err)
	}
}

func TestADrawsSnapshotUnderPausePerformsOneStepAndSaysSo(t *testing.T) {
	rig := newDrawsRig(t)
	rig.fixture.paused.Store(true)
	rig.fixture.on(func(queue *OpQueue) {
		queue.Text(1, "fonts/body.ttf", "frozen", TextDraw{Size: 10})
	})
	rig.fixture.onStep(func(k kernel.Kernel, _ app.TimeRequest) (app.TimeResponse, error) {
		k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
		return app.TimeResponse{Paused: true, Stepped: 1}, nil
	})

	// No tick is driven here: a paused engine runs none of its own, so the step
	// the capability raises is the only one, and the queue between ticks is
	// empty rather than stale.
	response, err := drawsSnapshot(rig.k, DrawsRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !response.Stepped {
		t.Error("a paused engine was stepped to produce the snapshot, and the response denies it")
	}
	if len(response.Ops) != 1 || response.Ops[0].Text != "frozen" {
		t.Fatalf("ops = %+v, want the one the step recorded", response.Ops)
	}
	var steps []app.TimeRequest
	for _, asked := range rig.fixture.asked() {
		if asked.Action == app.TimeStep {
			steps = append(steps, asked)
		}
	}
	if len(steps) != 1 {
		t.Fatalf("steps raised = %d, want exactly one", len(steps))
	}
	// Join is what makes three arms share one step rather than taking three
	// ticks, which is the whole of the pairing recipe.
	if steps[0].Steps != 1 || !steps[0].Join {
		t.Errorf("step = %+v, want one tick asked for with Join set", steps[0])
	}
}

func TestADrawsSnapshotJoiningAPendingStepSaysThatToo(t *testing.T) {
	rig := newDrawsRig(t)
	rig.fixture.paused.Store(true)
	rig.fixture.on(func(queue *OpQueue) {
		queue.Text(1, "fonts/body.ttf", "shared", TextDraw{Size: 10})
	})
	rig.fixture.onStep(func(k kernel.Kernel, _ app.TimeRequest) (app.TimeResponse, error) {
		k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
		return app.TimeResponse{Paused: true, Stepped: 1, Joined: true}, nil
	})

	response, err := drawsSnapshot(rig.k, DrawsRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !response.Stepped || !response.Joined {
		t.Errorf("stepped=%v joined=%v, want both: the tick happened and it was somebody else's",
			response.Stepped, response.Joined)
	}
}

func TestADrawsSnapshotIsWrittenToThePathTheAgentNames(t *testing.T) {
	rig := newDrawsRig(t)
	rig.fixture.on(aSpriteAndTriangles)
	path := filepath.Join(t.TempDir(), "nested", "draws.json")

	response, err := rig.runDraws(DrawsRequest{Path: path})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if response.Path != path {
		t.Fatalf("response path = %q, want %q", response.Path, path)
	}
	// The ops are in the file; the reply still says how big the frame was and
	// what coordinate frame each layer is in, so the agent knows whether
	// opening it is worth a turn.
	if response.Ops != nil {
		t.Error("the op array was returned inline as well as written, doubling the reply")
	}
	if response.OpCount != 3 || len(response.Layers) != 2 {
		t.Errorf("counts = %d ops / %d layers, want them kept inline",
			response.OpCount, len(response.Layers))
	}

	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the written snapshot: %v", err)
	}
	var written DrawsResponse
	if err := json.Unmarshal(document, &written); err != nil {
		t.Fatalf("the written snapshot is not JSON: %v", err)
	}
	if len(written.Ops) != 3 || written.Ops[0].Kind != "sprite" {
		t.Fatalf("the file holds %+v, want the whole frame in record order", written.Ops)
	}
	if written.Path != path {
		t.Errorf("the file names itself %q, want %q", written.Path, path)
	}
}

func TestADrawsSnapshotRefusesARequestItCannotHonour(t *testing.T) {
	rig := newDrawsRig(t)
	from, to := 4, 2
	for _, test := range []struct {
		name    string
		request DrawsRequest
		wants   string
	}{
		{"relative path", DrawsRequest{Path: filepath.Join("draws", "one.json")}, "absolute"},
		{"wrong extension", DrawsRequest{Path: filepath.Join(t.TempDir(), "draws.txt")}, ".json"},
		{"unknown kind", DrawsRequest{Kinds: []string{"sprites"}}, "not a draw kind"},
		{"inverted range", DrawsRequest{FromLayer: &from, ToLayer: &to}, "keeps no layer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := drawsSnapshot(rig.k, test.request)
			var refusal mcp.Unavailable
			if !errors.As(err, &refusal) {
				t.Fatalf("answered %v, want words an agent can act on", err)
			}
			if !strings.Contains(refusal.Reason, test.wants) {
				t.Errorf("reason = %q, want it to mention %q", refusal.Reason, test.wants)
			}
		})
	}
}

func TestADrawsSnapshotHoldsNothingThatAliasesTheQueue(t *testing.T) {
	rig := newDrawsRig(t)
	rig.fixture.on(aSpriteAndTriangles)

	// The vertex drill-down is what puts the queue's own vertex arena in the
	// reply, so it is the case worth pinning here.
	response, err := rig.runDraws(DrawsRequest{Vertices: []int{2}})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	before := marshalDraws(t, response)

	// The queue's arenas are reused from the first byte every tick, so a view
	// still pointing into one - a parameter list, a vertex list, an op slice -
	// reads as whatever the next frames recorded. Ops documents the aliasing
	// (canvas/inspect.go), which is exactly why the snapshot renders into
	// owned values inside the tick rather than keeping what it was handed.
	rig.fixture.on(func(queue *OpQueue) {
		for i := range 20 {
			queue.Sprite(Layer(i), "images/other.png", SpriteTransform{
				Position: m.Vec2{X: float32(i) * 3, Y: 7}, Scale: 2,
			}, nil, gfx.FloatParam("noise", float32(i)))
			queue.DrawTriangles(Layer(i), triangleFan(), nil)
		}
	})
	for range 3 {
		rig.tick()
	}

	if after := marshalDraws(t, response); after != before {
		t.Errorf("the snapshot changed when the queue was recorded into again:\nwas  %s\nnow  %s",
			before, after)
	}
}

func TestADrawsSnapshotReportsATextureParameterWithoutItsPixels(t *testing.T) {
	rig := newDrawsRig(t)
	pixels := make([]byte, 16*16*4)
	rig.fixture.on(func(queue *OpQueue) {
		texture := gfx.TextureWithBytes(16, 16, gfx.FormatRGBA8, pixels, false, false)
		queue.SpriteTexture(1, texture, SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil,
			gfx.TextureParam("mask", texture))
	})

	response, err := rig.runDraws(DrawsRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(response.Ops) != 1 {
		t.Fatalf("ops = %d, want the one recorded", len(response.Ops))
	}
	op := response.Ops[0]
	if op.Texture == nil || op.Texture.Width != 16 || op.Texture.Bytes != len(pixels) {
		t.Fatalf("texture = %+v, want its size and a byte count", op.Texture)
	}
	if len(op.Params) != 1 || op.Params[0].Texture == nil {
		t.Fatalf("params = %+v, want the texture parameter resolved", op.Params)
	}
	// Bulk bytes never travel: an inline texture in a reply is a base64
	// megabyte nobody asked for, so the count goes and the pixels stay.
	document := marshalDraws(t, response)
	if strings.Contains(document, "pixels") || strings.Contains(document, "AAAAAA") {
		t.Errorf("the response carries pixel data: %s", document)
	}
	// And a tagged union serializes to exactly one value, never the dead half
	// beside the live one.
	parameter := decodeFirstParam(t, document)
	for _, dead := range []string{"value", "sampler", "buffer", "bytes"} {
		if _, present := parameter[dead]; present {
			t.Errorf("a texture parameter carries %q as well as its texture", dead)
		}
	}
}

func TestADrawsSnapshotIsOneFlatDocument(t *testing.T) {
	rig := newDrawsRig(t)
	rig.fixture.on(aSpriteAndTriangles)
	response, err := rig.runDraws(DrawsRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// The embedded views are inlined rather than nested, so an agent reads one
	// object instead of reaching through two it was never told about.
	var document map[string]any
	if err := json.Unmarshal([]byte(marshalDraws(t, response)), &document); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{
		"ops", "layers", "opCount", "layerCount",
		"pixelWidth", "pixelHeight", "windowWidth", "windowHeight",
		"viewportWidth", "viewportHeight", "stepped",
	} {
		if _, present := document[key]; !present {
			t.Errorf("the response has no top-level %q", key)
		}
	}
	for _, absent := range []string{"DrawsView", "SnapshotView"} {
		if _, present := document[absent]; present {
			t.Errorf("the response nests %q instead of inlining it", absent)
		}
	}
}

func TestCanvasOffersOneReadOnlyCapability(t *testing.T) {
	offered := New().Capabilities()
	if len(offered) != 1 {
		t.Fatalf("capabilities = %d, want the one canvas implements", len(offered))
	}
	capability := offered[0]
	if err := capability.Err(); err != nil {
		t.Fatalf("%s failed construction: %v", drawsName, err)
	}
	if capability.Name() != drawsName {
		t.Fatalf("name = %q, want %q", capability.Name(), drawsName)
	}
	// ReadOnly in cog's reading means the capability does not change the game.
	// The step it costs under pause is stated in the description, because mcp
	// annotates a tool rather than an argument.
	if !capability.ReadOnly() {
		t.Error("canvas_draws changes nothing in the game, and is read-only in cog's reading")
	}
	if capability.Description() != drawsDescription {
		t.Error("the description an agent reads is not the one the spec reproduces")
	}
}

// withGfxResources runs use inside a handler holding gfx's resource queue, so
// a test can allocate the render target a layer draws into the way an app
// does.
func withGfxResources(t *testing.T, k kernel.Executioner, use func(*gfx.ResourceQueue)) {
	t.Helper()
	k.ExecuteCommand[gfxResourceProbeCmd](gfxResourceProbeRequest{run: use})
}

type gfxResourceProbeCmd kernel.Command[gfxResourceProbeRequest, gfxResourceProbeResponse]
type gfxResourceProbeRequest struct{ run func(*gfx.ResourceQueue) }
type gfxResourceProbeResponse struct{}

func gfxResourceProbeCmdImpl() (kernel.Lock, kernel.Execute[gfxResourceProbeRequest, gfxResourceProbeResponse]) {
	var resources kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*gfx.ResourceQueue]()
		}, func(_ kernel.Kernel, request gfxResourceProbeRequest) (gfxResourceProbeResponse, error) {
			request.run(resources.Get())
			return gfxResourceProbeResponse{}, nil
		}
}

func marshalDraws(t *testing.T, response DrawsResponse) string {
	t.Helper()
	document, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(document)
}

// decodeFirstParam pulls the first op's first parameter back out of the
// rendered document, so an assertion about what a union emitted is made
// against the JSON rather than against the struct.
func decodeFirstParam(t *testing.T, document string) map[string]any {
	t.Helper()
	var decoded struct {
		Ops []struct {
			Params []map[string]any `json:"params"`
		} `json:"ops"`
	}
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Ops) == 0 || len(decoded.Ops[0].Params) == 0 {
		t.Fatalf("the document carries no parameter: %s", document)
	}
	return decoded.Ops[0].Params[0]
}

func equalFloat32s(got, want []float32) bool {
	return slices.Equal(got, want)
}
