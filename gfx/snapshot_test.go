package gfx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/mcp"
)

// A frame snapshot is the renderer answering "why is nothing on screen", so
// the properties worth pinning are the ones that would make the answer a lie:
// a snapshot of the tick before the request, an index that stops addressing
// what it named once a filter is on, and a paused engine that either hangs or
// silently never steps.

// flushPlugin stands in for canvas: it records into the gfx queue from the
// Last phase, ordered Before gfx's own present handler, which is exactly where
// canvas puts its flush (canvas/plugin.go). gfx cannot name
// canvas.UpdateEventHandler - canvas imports gfx, not the other way round - so
// this fixture is how the ordering claim the snapshot rests on gets asserted
// in gfx's own tests. It is inert until a test gives it something to record.
type flushPlugin struct {
	mu     sync.Mutex
	record func(*OpQueue)
}

type flushUpdateHandler kernel.Subscription[app.UpdateEvent]

func (*flushPlugin) Name() kernel.PluginName           { return "gfxtestflush" }
func (*flushPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (f *flushPlugin) Register(r *kernel.Registrar, _ any) error {
	r.Subscribe[flushUpdateHandler](f.flushOnUpdate).Last().Before[UpdateEventHandler]()
	return nil
}

func (f *flushPlugin) flushOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
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

func (f *flushPlugin) on(record func(*OpQueue)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record = record
}

// runSnapshot calls the capability body on its own goroutine and drives ticks
// until it answers, recording what the test asked for into each one. That is
// what an agent's call looks like from the engine's side.
func (r *captureRig) runSnapshot(request FrameRequest, record func(*OpQueue)) (FrameResponse, error) {
	r.t.Helper()
	type answer struct {
		response FrameResponse
		err      error
	}
	done := make(chan answer, 1)
	go func() {
		response, err := frameSnapshot(r.k, request)
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
		record(recordRaw(r.t, r.k))
		r.tick()
		time.Sleep(time.Millisecond)
	}
}

// twoPasses records a frame whose declaration order is the reverse of its run
// order, which is the only way to tell the two apart.
func twoPasses(q *OpQueue) {
	q.Pass(PassDescr{
		Target: ScreenTarget(), Depth: DepthAuto(), Order: 20, Label: "overlay",
		Load: LoadPreserve, Store: StoreKeep,
	})
	drawInto(q)
	q.Pass(PassDescr{
		Target: ScreenTarget(), Depth: DepthAuto(), Order: 10, Label: "world",
		Load: LoadClear, Clear: m.Color{R: 0.25, A: 1}, Store: StoreKeep,
	})
	drawInto(q)
	q.DrawInstanced(triangle(), testMaterial(), 7, MatParam("mvp", m.NewMat4()))
}

func TestAFrameSnapshotReportsPassesInRunOrderWithTheirCounts(t *testing.T) {
	rig := newCaptureRig(t)

	response, err := rig.runSnapshot(FrameRequest{}, twoPasses)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(response.Passes) != 2 {
		t.Fatalf("passes = %d, want the two the frame declared", len(response.Passes))
	}
	// Passes run in Order, ties broken by declaration sequence - never in the
	// order they were declared, which is what "a pass ordered wrong" is about.
	world, overlay := response.Passes[0], response.Passes[1]
	if world.Label != "world" || overlay.Label != "overlay" {
		t.Fatalf("run order = %q then %q, want world then overlay", world.Label, overlay.Label)
	}
	if world.Index != 1 || world.Run != 0 || overlay.Index != 0 || overlay.Run != 1 {
		t.Errorf("indices = world %d/run %d, overlay %d/run %d; want the declaration index beside "+
			"the run position", world.Index, world.Run, overlay.Index, overlay.Run)
	}
	if world.Draws != 2 || world.Instances != 8 {
		t.Errorf("world = %d draws / %d instances, want 2 and 8", world.Draws, world.Instances)
	}
	if overlay.Draws != 1 || overlay.Instances != 1 {
		t.Errorf("overlay = %d draws / %d instances, want 1 and 1", overlay.Draws, overlay.Instances)
	}
	if world.Target != "screen" || world.Depth != "auto" {
		t.Errorf("world target/depth = %q/%q, want screen/auto", world.Target, world.Depth)
	}
	if world.Load != "clear" || !equalFloats(world.Clear, []float32{0.25, 0, 0, 1}) {
		t.Errorf("world clears %q with %v, want clear with the colour it declared",
			world.Load, world.Clear)
	}
	if overlay.Load != "preserve" || overlay.Clear != nil {
		t.Errorf("overlay clears %q with %v, want preserve and no colour",
			overlay.Load, overlay.Clear)
	}
	if response.PassCount != 2 || response.DrawCount != 3 || response.InstanceCount != 9 {
		t.Errorf("frame totals = %d passes / %d draws / %d instances, want 2 / 3 / 9",
			response.PassCount, response.DrawCount, response.InstanceCount)
	}
	// The three coordinate sizes the rig set, including the logical one no
	// other response reports.
	if response.PixelWidth != 1600 || response.PixelHeight != 1200 {
		t.Errorf("pixels = %dx%d, want 1600x1200", response.PixelWidth, response.PixelHeight)
	}
	if response.WindowWidth != 800 || response.WindowHeight != 600 {
		t.Errorf("window = %vx%v, want 800x600", response.WindowWidth, response.WindowHeight)
	}
	if response.ViewportWidth != 800 || response.ViewportHeight != 600 {
		t.Errorf("viewport = %vx%v, want the logical size resolved from the window",
			response.ViewportWidth, response.ViewportHeight)
	}
	if response.Stepped {
		t.Error("a running engine was reported as having been stepped")
	}
}

func TestAFilteredSnapshotKeepsSourceIndicesAndNamesWhatItDropped(t *testing.T) {
	rig := newCaptureRig(t)

	whole, err := rig.runSnapshot(FrameRequest{}, twoPasses)
	if err != nil {
		t.Fatalf("unfiltered snapshot: %v", err)
	}
	filtered, err := rig.runSnapshot(FrameRequest{Pass: "world"}, twoPasses)
	if err != nil {
		t.Fatalf("filtered snapshot: %v", err)
	}
	if len(filtered.Passes) != 1 || filtered.Passes[0].Label != "world" {
		t.Fatalf("filtered passes = %+v, want only world", filtered.Passes)
	}
	// The index is the address: it is a position in the queue that declared
	// the pass, never a position in the emitted array, so an index read off a
	// filtered response still names the same pass in an unfiltered one.
	if filtered.Passes[0].Index != whole.Passes[0].Index {
		t.Errorf("filtered index = %d, unfiltered = %d; filtering moved the address",
			filtered.Passes[0].Index, whole.Passes[0].Index)
	}
	if filtered.Passes[0].Run != whole.Passes[0].Run {
		t.Errorf("filtered run = %d, unfiltered = %d; the run position is the whole frame's",
			filtered.Passes[0].Run, whole.Passes[0].Run)
	}
	if filtered.OmittedPasses != 1 || filtered.Filter != "world" {
		t.Errorf("filter = %q dropping %d, want world dropping 1 - what is omitted is named",
			filtered.Filter, filtered.OmittedPasses)
	}
	// And the totals still describe the whole frame, so the agent can see how
	// much of it the filter hid.
	if filtered.PassCount != whole.PassCount || filtered.DrawCount != whole.DrawCount {
		t.Errorf("filtered totals = %d passes / %d draws, want the whole frame's %d / %d",
			filtered.PassCount, filtered.DrawCount, whole.PassCount, whole.DrawCount)
	}
}

func TestAFrameSnapshotReportsResourceTrafficFromBothQueues(t *testing.T) {
	rig := newCaptureRig(t)
	var durable TextureDescr
	withResourceQueue(t, rig.k, func(q *ResourceQueue) {
		durable = q.AllocateTexture(64, 32, 2, FormatRGBA8)
		q.UpdateTexture(durable, 1, Region{X: 1, Y: 2, Width: 3, Height: 4}, make([]byte, 48), true)
		q.ReleaseTexture(q.BakeTexture(8, 8, FormatRGBA8Srgb, make([]byte, 256), true, true))
	})

	response, err := rig.runSnapshot(FrameRequest{}, func(q *OpQueue) {
		q.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Label: "screen"})
		// An inline texture is baked into the frame queue, which is the other
		// place resource traffic comes from.
		q.Draw(triangle(), testMaterial(),
			MatParam("mvp", m.NewMat4()),
			TextureParam("albedo", TextureWithBytes(4, 4, FormatRGBA8, make([]byte, 64), true, false)))
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	byKind := map[string][]ResourceOpView{}
	for _, op := range response.ResourceOps {
		byKind[op.Kind] = append(byKind[op.Kind], op)
	}
	allocate := byKind["allocateTexture"]
	if len(allocate) != 1 {
		t.Fatalf("allocateTexture ops = %d, want the durable one", len(allocate))
	}
	if allocate[0].Queue != "durable" || allocate[0].Texture != durable.ID() {
		t.Errorf("allocate = %+v, want the durable queue and the texture it returned", allocate[0])
	}
	if allocate[0].Width != 64 || allocate[0].Height != 32 || allocate[0].Layers != 2 {
		t.Errorf("allocate size = %dx%dx%d, want 64x32x2",
			allocate[0].Width, allocate[0].Height, allocate[0].Layers)
	}
	update := byKind["updateTexture"]
	if len(update) != 1 || update[0].Region == nil {
		t.Fatalf("updateTexture ops = %+v, want the one with its region", update)
	}
	if *update[0].Region != (Region{X: 1, Y: 2, Width: 3, Height: 4}) || update[0].Layer != 1 {
		t.Errorf("update = %+v, want the layer and region it was given", update[0])
	}
	if update[0].Bytes != 48 {
		t.Errorf("update bytes = %d, want the 48 uploaded and none of them inline", update[0].Bytes)
	}
	if release := byKind["releaseTexture"]; len(release) != 1 {
		t.Errorf("releaseTexture ops = %d, want the one queued - a resource released and still "+
			"referenced is one of the answers this tool exists for", len(release))
	}
	bakes := byKind["bakeTexture"]
	if len(bakes) != 2 {
		t.Fatalf("bakeTexture ops = %d, want the durable one and the frame's inline one", len(bakes))
	}
	if bakes[0].Queue != "durable" || bakes[1].Queue != "frame" {
		t.Errorf("bake queues = %q then %q, want durable first, which is the order they execute in",
			bakes[0].Queue, bakes[1].Queue)
	}
	if !bakes[0].Mipmaps || bakes[0].Format != formatName(FormatRGBA8Srgb) {
		t.Errorf("durable bake = %+v, want its format and mipmap flag", bakes[0])
	}
	if bakes[1].Bytes != 64 || bakes[1].Width != 4 {
		t.Errorf("frame bake = %+v, want the 4x4 inline texture reported as 64 bytes", bakes[1])
	}
	// Every op is addressed by its own queue's index, and no index is a lie
	// about which queue it belongs to.
	for _, op := range response.ResourceOps {
		if op.Queue != "durable" && op.Queue != "frame" {
			t.Errorf("op %+v names no queue", op)
		}
	}
}

func TestAFrameSnapshotBindsToATickThatBeganAfterTheRequest(t *testing.T) {
	rig := newCaptureRig(t)
	before := recordRaw(t, rig.k)
	before.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Label: "before"})
	drawInto(before)
	release := rig.gate.open()

	// The arm lands inside a tick that is already running, which is the case a
	// between-ticks arm cannot produce and the one a naive implementation gets
	// wrong: that tick's queue was recorded before the request was made.
	ticked := make(chan struct{})
	go func() {
		rig.k.PublishEvent(app.UpdateEvent{Last: true}).Wait()
		close(ticked)
	}()
	<-rig.gate.entered
	armed, err := rig.k.ExecuteCommand[ArmFrameCmd](ArmFrameRequest{})
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	release()
	<-ticked

	select {
	case snapshot := <-armed.Done:
		t.Fatalf("the snapshot described the tick it was armed inside: %+v", snapshot.Frame.Passes)
	default:
	}

	after := recordRaw(t, rig.k)
	after.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Label: "after"})
	drawInto(after)
	rig.tick()

	select {
	case snapshot := <-armed.Done:
		if snapshot.Err != nil {
			t.Fatalf("snapshot failed: %v", snapshot.Err)
		}
		if len(snapshot.Frame.Passes) != 1 || snapshot.Frame.Passes[0].Label != "after" {
			t.Fatalf("the snapshot describes %+v, want the pass recorded after the request",
				snapshot.Frame.Passes)
		}
	default:
		t.Fatal("the tick after the request produced no snapshot")
	}
}

func TestASecondFrameSnapshotIsRefusedInWordsWhileOneIsInFlight(t *testing.T) {
	rig := newCaptureRig(t)
	if _, err := rig.k.ExecuteCommand[ArmFrameCmd](ArmFrameRequest{}); err != nil {
		t.Fatalf("first arm: %v", err)
	}

	_, err := frameSnapshot(rig.k, FrameRequest{})
	var refusal mcp.Unavailable
	if !errors.As(err, &refusal) {
		t.Fatalf("a second snapshot answered %v, want words an agent can act on", err)
	}
	if !strings.Contains(refusal.Reason, "already in flight") {
		t.Errorf("reason = %q, want it to say one is already in flight", refusal.Reason)
	}

	// A capture and the other packages' snapshots are separate slots. Refusing
	// across kinds would destroy the pairing arming them together is for.
	if _, err := rig.k.ExecuteCommand[ArmCaptureCmd](ArmCaptureRequest{
		Target: GpuCaptureDesc{Screen: true},
	}); err != nil {
		t.Fatalf("a capture was refused while a frame snapshot was in flight: %v", err)
	}
}

func TestAFrameSnapshotUnderPausePerformsOneStepAndSaysSo(t *testing.T) {
	rig := newCaptureRig(t)
	rig.clock.paused.Store(true)
	rig.clock.onStep(func(k kernel.Kernel, _ app.TimeRequest) (app.TimeResponse, error) {
		k.PublishEvent(app.UpdateEvent{Last: true}).Wait()
		return app.TimeResponse{Paused: true, Stepped: 1}, nil
	})
	// Recorded before the call and never ticked away: a paused engine runs no
	// tick of its own, so the step the capability raises is the only one.
	frozen := recordRaw(t, rig.k)
	frozen.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Label: "frozen"})
	drawInto(frozen)

	response, err := frameSnapshot(rig.k, FrameRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !response.Stepped {
		t.Error("a paused engine was stepped to produce the snapshot, and the response denies it")
	}
	if len(response.Passes) != 1 || response.Passes[0].Label != "frozen" {
		t.Fatalf("passes = %+v, want the one the step recorded", response.Passes)
	}
	var steps []app.TimeRequest
	for _, asked := range rig.clock.asked() {
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

func TestAFrameSnapshotJoiningAPendingStepSaysThatToo(t *testing.T) {
	rig := newCaptureRig(t)
	rig.clock.paused.Store(true)
	rig.clock.onStep(func(k kernel.Kernel, _ app.TimeRequest) (app.TimeResponse, error) {
		k.PublishEvent(app.UpdateEvent{Last: true}).Wait()
		return app.TimeResponse{Paused: true, Stepped: 1, Joined: true}, nil
	})
	frozen := recordRaw(t, rig.k)
	frozen.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Label: "shared"})

	response, err := frameSnapshot(rig.k, FrameRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !response.Stepped || !response.Joined {
		t.Errorf("stepped=%v joined=%v, want both: the tick happened and it was somebody else's",
			response.Stepped, response.Joined)
	}
}

func TestAFrameSnapshotIsWrittenToThePathTheAgentNames(t *testing.T) {
	rig := newCaptureRig(t)
	path := filepath.Join(t.TempDir(), "nested", "frame.json")

	response, err := rig.runSnapshot(FrameRequest{Path: path}, twoPasses)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if response.Path != path {
		t.Fatalf("response path = %q, want %q", response.Path, path)
	}
	// The detail is in the file; the reply still says what the frame was, so
	// the agent knows whether opening it is worth a turn.
	if response.Passes != nil || response.ResourceOps != nil {
		t.Error("the arrays were returned inline as well as written, doubling the reply")
	}
	if response.PassCount != 2 || response.DrawCount != 3 {
		t.Errorf("counts = %d passes / %d draws, want them kept inline",
			response.PassCount, response.DrawCount)
	}

	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the written snapshot: %v", err)
	}
	var written FrameResponse
	if err := json.Unmarshal(document, &written); err != nil {
		t.Fatalf("the written snapshot is not JSON: %v", err)
	}
	if len(written.Passes) != 2 || written.Passes[0].Label != "world" {
		t.Fatalf("the file holds %+v, want the whole frame in run order", written.Passes)
	}
	if written.Path != path {
		t.Errorf("the file names itself %q, want %q", written.Path, path)
	}
}

func TestAFrameSnapshotRefusesAPathItCannotHonour(t *testing.T) {
	rig := newCaptureRig(t)
	for _, test := range []struct{ name, path, wants string }{
		{"relative", filepath.Join("frames", "one.json"), "absolute"},
		{"wrong extension", filepath.Join(t.TempDir(), "frame.txt"), ".json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := frameSnapshot(rig.k, FrameRequest{Path: test.path})
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

func TestAFrameSnapshotIsOneFlatDocument(t *testing.T) {
	rig := newCaptureRig(t)
	response, err := rig.runSnapshot(FrameRequest{}, twoPasses)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// The embedded views are inlined rather than nested, so an agent reads one
	// object instead of reaching through two it was never told about.
	document := marshalToMap(t, response)
	for _, key := range []string{
		"passes", "passCount", "drawCount", "instanceCount",
		"pixelWidth", "pixelHeight", "windowWidth", "windowHeight",
		"viewportWidth", "viewportHeight", "stepped",
	} {
		if _, present := document[key]; !present {
			t.Errorf("the response has no top-level %q", key)
		}
	}
	for _, absent := range []string{"FrameView", "SnapshotView"} {
		if _, present := document[absent]; present {
			t.Errorf("the response nests %q instead of inlining it", absent)
		}
	}
}

func TestAFrameSnapshotNamesDrawsThatBelongToNoPass(t *testing.T) {
	rig := newCaptureRig(t)
	response, err := rig.runSnapshot(FrameRequest{}, func(q *OpQueue) {
		// A draw recorded before any pass is dropped by the renderer and
		// reported as ErrDrawWithoutPass. It is also one of the reasons a
		// frame is black, so the snapshot says it rather than counting to
		// zero.
		drawInto(q)
		q.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Label: "empty"})
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if response.StrayDraws != 1 {
		t.Errorf("stray draws = %d, want the one recorded outside every pass", response.StrayDraws)
	}
	if len(response.Passes) != 1 {
		t.Fatalf("passes = %d, want the declared one", len(response.Passes))
	}
	// A pass with no draws that loads nothing never reaches the GPU, which is
	// a different answer from a pass that ran and drew nothing.
	if response.Passes[0].Runs {
		t.Error("an empty preserve-everything pass is reported as running, and the translator skips it")
	}
}

func TestAFrameSnapshotSeesWhatALateRecorderFlushedIntoTheQueue(t *testing.T) {
	rig := newCaptureRig(t)
	// canvas flushes from the Last phase, ordered before gfx's present. The
	// snapshot has to be after that and before the swap, or it describes a
	// frame missing everything the canvas drew - which is precisely the frame
	// an agent is asking about.
	rig.flush.on(func(q *OpQueue) {
		q.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Order: 100, Label: "canvas"})
		drawInto(q)
	})
	t.Cleanup(func() { rig.flush.on(nil) })

	response, err := rig.runSnapshot(FrameRequest{}, func(q *OpQueue) {
		q.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Order: 10, Label: "world"})
		drawInto(q)
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	labels := make([]string, 0, len(response.Passes))
	for _, pass := range response.Passes {
		labels = append(labels, pass.Label)
	}
	if !slices.Equal(labels, []string{"world", "canvas"}) {
		t.Fatalf("passes = %v, want the app's and the late recorder's, in run order", labels)
	}
	if response.DrawCount != 2 {
		t.Errorf("draws = %d, want both recorders' ", response.DrawCount)
	}
}

func TestTheNewAccessorsAnswerOutsideTheAgentPath(t *testing.T) {
	// They are ordinary gfx API, not a debug back door: nothing here goes near
	// a view, a capability or a JSON document.
	vertices := BufferWithBytes(make([]byte, 3*32), false)
	indices := BufferWithBytes(make([]byte, 6*4), false)
	mesh := MeshIndexed(vertices, indices, TopologyTriangleList,
		Attr(0, Float32x4), Attr(16, Float32x4))
	if mesh.VertexCount() != 3 || mesh.IndexCount() != 6 {
		t.Errorf("counts = %d vertices / %d indices, want 3 and 6",
			mesh.VertexCount(), mesh.IndexCount())
	}
	if !mesh.Indexed() || mesh.Topology() != TopologyTriangleList {
		t.Errorf("mesh = indexed %v / topology %v, want an indexed triangle list",
			mesh.Indexed(), mesh.Topology())
	}
	if vertices.Size() != 96 || vertices.InlineBytes() != 96 || vertices.ID() != 0 {
		t.Errorf("buffer = %d bytes / %d inline / id %d, want 96, 96 and no baked id",
			vertices.Size(), vertices.InlineBytes(), vertices.ID())
	}

	texture := TextureWithBytes(8, 4, FormatRGBA8Srgb, make([]byte, 128), false, true)
	if texture.Format() != FormatRGBA8Srgb || !texture.Mipmaps() || texture.PixelBytes() != 128 {
		t.Errorf("texture = format %v / mipmaps %v / %d bytes, want what it was built with",
			texture.Format(), texture.Mipmaps(), texture.PixelBytes())
	}

	// Each value accessor is keyed on the parameter's own kind, which is what
	// makes reading one arm of the union for another impossible rather than
	// merely unlikely.
	matrix := MatParam("mvp", m.NewMat4())
	if _, ok := matrix.MatValue(); !ok {
		t.Error("MatValue refused a mat4 parameter")
	}
	if _, ok := matrix.BufferValue(); ok {
		t.Error("BufferValue answered for a mat4 parameter")
	}
	if _, ok := matrix.RawLen(); ok {
		t.Error("RawLen answered for a mat4 parameter")
	}
	if _, _, ok := matrix.BufferRange(); ok {
		t.Error("BufferRange answered for a mat4 parameter")
	}
}

func TestAFrameSnapshotDescribesAPassDrawingSomewhereOtherThanTheScreen(t *testing.T) {
	rig := newCaptureRig(t)
	response, err := rig.runSnapshot(FrameRequest{}, func(q *OpQueue) {
		target, _ := q.TemporaryTarget(128, 64, FormatRGBA8)
		q.Pass(PassDescr{Target: target, Depth: DepthNone(), Label: "offscreen", Load: LoadClear})
		drawInto(q)
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(response.Passes) != 1 {
		t.Fatalf("passes = %d, want the one declared", len(response.Passes))
	}
	pass := response.Passes[0]
	if pass.Target != "texture" || pass.Depth != "none" {
		t.Errorf("target/depth = %q/%q, want texture/none", pass.Target, pass.Depth)
	}
	if pass.TargetWidth != 128 || pass.TargetHeight != 64 || pass.TargetTexture == 0 {
		t.Errorf("target = %+v, want the 128x64 texture it allocated", pass)
	}
	if !pass.Runs {
		t.Error("a pass with a draw in it is reported as not running")
	}
}
