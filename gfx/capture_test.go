package gfx

import (
	"context"
	"errors"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/mcp"
	"github.com/dvoyni/cog/storage"
)

// A capture is the first thing in cog that reads a rendered pixel back, and
// the properties worth pinning are the ones that are invisible when they
// break: an un-stride that shears the image by one row of padding, a capture
// that binds to the tick before the request rather than the one after it, and
// a waiter that learns nothing when the window closes underneath it.

// captureRig is a gfx engine a capture test drives by hand: one tick is an
// app.UpdateEvent, one frame an app.RenderEvent, and the fake backend hands
// readbacks back with the real one's one frame of latency.
type captureRig struct {
	t       *testing.T
	k       kernel.Executioner
	plugin  *Plugin
	backend *fakeBackend
	clock   *timePlugin
	gate    *gatePlugin
	flush   *flushPlugin
}

func newCaptureRig(t *testing.T) *captureRig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	plugin, clock, gate, flush := New(), &timePlugin{}, &gatePlugin{}, &flushPlugin{}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("gfx-capture-test"),
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(storage.New(), plugin, clock, gate, flush, testPlugin{})
	stopped := make(chan struct{})
	go func() { engine.Run(ctx); close(stopped) }()
	<-engine.Ready()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})

	rig := &captureRig{
		t: t, k: engine.Executioner(), plugin: plugin,
		backend: &fakeBackend{}, clock: clock, gate: gate, flush: flush,
	}
	rig.k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: rig.backend})
	rig.k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	return rig
}

// record puts one labelled screen pass in the writable queue, so the frame a
// capture rides is identifiable by what it drew.
func (r *captureRig) record(label string) {
	r.t.Helper()
	q := recordRaw(r.t, r.k)
	q.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear, Label: label})
	drawInto(q)
}

func (r *captureRig) tick() {
	r.t.Helper()
	r.k.PublishEvent(app.UpdateEvent{Last: true}).Wait()
}

func (r *captureRig) render() {
	r.t.Helper()
	r.k.PublishEvent(app.RenderEvent{}).Wait()
}

// frame is one whole turn of the engine: something recorded, a tick, a render.
func (r *captureRig) frame(label string) {
	r.t.Helper()
	r.record(label)
	r.tick()
	r.render()
}

// runCapture calls the capability body on its own goroutine and drives frames
// until it answers, which is what an agent's call looks like from the engine's
// side.
func (r *captureRig) runCapture(request CaptureRequest) (CaptureResponse, error) {
	r.t.Helper()
	type answer struct {
		response CaptureResponse
		err      error
	}
	done := make(chan answer, 1)
	go func() {
		response, err := captureScreen(r.k, request)
		done <- answer{response, err}
	}()
	expiry := time.After(10 * time.Second)
	for {
		select {
		case got := <-done:
			return got.response, got.err
		case <-expiry:
			r.t.Fatal("the capture never answered")
		default:
		}
		r.frame("screen")
		time.Sleep(time.Millisecond)
	}
}

// timePlugin answers app.TimeCmd so a test can tell the capability that the
// tick source is stopped, and can stand in for the host when something steps
// it. The real answer belongs to whichever host owns the loop, which a gfx
// test does not have.
type timePlugin struct {
	paused atomic.Bool

	mu sync.Mutex
	// step, when set, is what a TimeStep does. A fixture host publishes the
	// ticks the real tick source would; nil means a step reports and does
	// nothing, which is what the capture tests want.
	step func(kernel.Kernel, app.TimeRequest) (app.TimeResponse, error)
	// requests records what was asked of the tick source, so a test can assert
	// on the shape of the step a capability raised rather than only on its
	// effect.
	requests []app.TimeRequest
}

func (*timePlugin) Name() kernel.PluginName           { return "gfxtesttime" }
func (*timePlugin) Dependencies() []kernel.PluginName { return nil }

func (t *timePlugin) Register(r *kernel.Registrar, _ any) error {
	r.HandleCommand[app.TimeCmd](t.timeCmdImpl)
	return nil
}

func (t *timePlugin) timeCmdImpl() (kernel.Lock, kernel.Execute[app.TimeRequest, app.TimeResponse]) {
	return nil, func(k kernel.Kernel, request app.TimeRequest) (app.TimeResponse, error) {
		t.mu.Lock()
		t.requests = append(t.requests, request)
		step := t.step
		t.mu.Unlock()
		if request.Action == app.TimeStep && step != nil {
			return step(k, request)
		}
		return app.TimeResponse{Paused: t.paused.Load()}, nil
	}
}

// onStep installs what a step does, and asked reports what the tick source was
// asked for.
func (t *timePlugin) onStep(step func(kernel.Kernel, app.TimeRequest) (app.TimeResponse, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.step = step
}

func (t *timePlugin) asked() []app.TimeRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.requests)
}

// gatePlugin holds the first phase of a tick open, so a test can make an arm
// land inside a tick that is already running - the one case the pending stage
// exists for, and the one a between-ticks arm cannot exercise.
type gatePlugin struct {
	entered chan struct{}
	release chan struct{}
}

type gateUpdateHandler kernel.Subscription[app.UpdateEvent]

func (*gatePlugin) Name() kernel.PluginName           { return "gfxtestgate" }
func (*gatePlugin) Dependencies() []kernel.PluginName { return nil }
func (g *gatePlugin) Register(r *kernel.Registrar, _ any) error {
	r.Subscribe[gateUpdateHandler](g.gateOnUpdate)
	return nil
}

func (g *gatePlugin) gateOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) error {
		if g.entered == nil {
			return nil
		}
		g.entered <- struct{}{}
		<-g.release
		return nil
	}
}

// open arms the gate and returns the function that lets the tick finish.
func (g *gatePlugin) open() func() {
	g.entered, g.release = make(chan struct{}), make(chan struct{})
	return func() {
		close(g.release)
		g.entered, g.release = nil, nil
	}
}

func TestGfxOffersItsTwoCapabilitiesAsReadOnly(t *testing.T) {
	offered := New().Capabilities()
	if len(offered) != 2 {
		t.Fatalf("capabilities = %d, want the two gfx implements", len(offered))
	}
	want := []struct {
		name, description string
	}{
		{captureName, captureDescription},
		{frameName, frameDescription},
	}
	for i, expected := range want {
		capability := offered[i]
		if err := capability.Err(); err != nil {
			t.Fatalf("%s failed construction: %v", expected.name, err)
		}
		if capability.Name() != expected.name {
			t.Fatalf("name = %q, want %q", capability.Name(), expected.name)
		}
		if !capability.ReadOnly() {
			t.Errorf("%s changes nothing in the game, and is read-only in cog's reading", expected.name)
		}
		if capability.Description() != expected.description {
			t.Errorf("the description an agent reads for %s is not the one the spec reproduces",
				expected.name)
		}
	}
}

func TestACaptureIsWrittenAsAPNGWithNoShear(t *testing.T) {
	// Five texels is twenty bytes a row, which the GPU pads to two hundred and
	// fifty-six: the width whose padding an un-stride that copies straight
	// through would smear across the image, one row further left each row.
	const width, height = 5, 3
	want := func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(10 + x*20), G: uint8(200 - y*50), B: uint8(x + y), A: 255}
	}
	rig := newCaptureRig(t)
	rig.backend.captureResult = func(GpuCaptureDesc) GpuCapture {
		return paddedCapture(width, height, want)
	}
	path := filepath.Join(t.TempDir(), "shot.png")

	response, err := rig.runCapture(CaptureRequest{Path: path})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if response.Path != path {
		t.Fatalf("response path = %q, want %q", response.Path, path)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open the written capture: %v", err)
	}
	defer file.Close()
	written, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode the written capture: %v", err)
	}
	if size := written.Bounds().Size(); size.X != width || size.Y != height {
		t.Fatalf("written size = %v, want %dx%d", size, width, height)
	}
	for y := range height {
		for x := range width {
			r, g, b, a := written.At(x, y).RGBA()
			expected := want(x, y)
			got := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
			if got != expected {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, expected)
			}
		}
	}
}

func TestACaptureReportsThePixelSizeAndTheWindowSize(t *testing.T) {
	rig := newCaptureRig(t)
	rig.backend.captureResult = func(GpuCaptureDesc) GpuCapture {
		return paddedCapture(64, 48, func(int, int) color.NRGBA { return color.NRGBA{A: 255} })
	}

	response, err := rig.runCapture(CaptureRequest{Path: filepath.Join(t.TempDir(), "sizes.png")})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if response.PixelWidth != 64 || response.PixelHeight != 48 {
		t.Fatalf("pixel size = %dx%d, want 64x48 from the capture itself",
			response.PixelWidth, response.PixelHeight)
	}
	if response.WindowWidth != 800 || response.WindowHeight != 600 {
		t.Fatalf("window size = %vx%v, want 800x600 in the units input uses",
			response.WindowWidth, response.WindowHeight)
	}
}

func TestACaptureBindsToATickThatBeganAfterTheRequest(t *testing.T) {
	rig := newCaptureRig(t)
	rig.record("before")
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
	armed, err := rig.k.ExecuteCommand[ArmCaptureCmd](ArmCaptureRequest{
		Target: GpuCaptureDesc{Screen: true},
	})
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	release()
	<-ticked

	rig.render()
	if len(rig.backend.captureDescs) != 0 {
		t.Fatalf("the capture bound to the tick it was armed inside, which was recorded before the request")
	}

	rig.record("after")
	rig.tick()
	rig.render()
	if len(rig.backend.captureLabels) != 1 {
		t.Fatalf("capture ops = %d, want the one the tick after the request bound",
			len(rig.backend.captureLabels))
	}
	if labels := rig.backend.captureLabels[0]; !slices.Contains(labels, "after") {
		t.Fatalf("the captured frame drew %v, want the pass recorded after the request", labels)
	}

	// And the pixels arrive one frame later, through that frame's own submit.
	rig.render()
	select {
	case capture := <-armed.Done:
		if capture.Err != nil {
			t.Fatalf("capture failed: %v", capture.Err)
		}
	default:
		t.Fatal("the readback did not resolve through the next frame's submit")
	}
}

func TestNoCaptureCostsTheRenderNothing(t *testing.T) {
	rig := newCaptureRig(t)
	rig.frame("screen")
	rig.frame("screen")

	if len(rig.backend.captureDescs) != 0 {
		t.Fatalf("capture ops = %d with nothing armed, want none", len(rig.backend.captureDescs))
	}
	if rig.backend.takeCalls != 2 {
		t.Fatalf("TakeCapture calls = %d, want one per frame", rig.backend.takeCalls)
	}
	if _, capturing := rig.plugin.captures.target(); capturing {
		t.Fatal("the render path found a capture to encode with nothing armed")
	}
}

func TestASecondCaptureWhileOneIsInFlightIsRefused(t *testing.T) {
	rig := newCaptureRig(t)
	if _, err := rig.k.ExecuteCommand[ArmCaptureCmd](ArmCaptureRequest{
		Target: GpuCaptureDesc{Screen: true},
	}); err != nil {
		t.Fatalf("first arm: %v", err)
	}
	_, err := rig.k.ExecuteCommand[ArmCaptureCmd](ArmCaptureRequest{
		Target: GpuCaptureDesc{Screen: true},
	})
	if !errors.Is(err, ErrCaptureBusy{}) {
		t.Fatalf("second arm = %v, want it refused as busy rather than queued", err)
	}

	// And the agent reads the refusal as words rather than as a fault.
	refusal := captureRefusal(ErrCaptureBusy{}, 1, 1)
	var unavailable mcp.Unavailable
	if !errors.As(refusal, &unavailable) {
		t.Fatalf("refusal = %T, want words an agent can act on", refusal)
	}
}

func TestACaptureAbandonedByShutdownArrivesOnItsChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	plugin := New()
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("gfx-capture-shutdown"),
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(storage.New(), plugin)
	stopped := make(chan struct{})
	go func() { engine.Run(ctx); close(stopped) }()
	<-engine.Ready()

	armed, err := engine.Executioner().ExecuteCommand[ArmCaptureCmd](ArmCaptureRequest{
		Target: GpuCaptureDesc{Screen: true},
	})
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	cancel()
	<-stopped

	select {
	case capture := <-armed.Done:
		if !errors.Is(capture.Err, ErrCaptureAbandoned{}) {
			t.Fatalf("abandoned capture = %v, want it reported as abandoned", capture.Err)
		}
	default:
		t.Fatal("shutdown left the waiter with nothing on the channel a result would have used")
	}
}

func TestDepthAndOtherFormatsAreNotAnImage(t *testing.T) {
	for _, format := range []TextureFormat{FormatDepth32F, TextureFormat(99)} {
		capture := GpuCapture{
			Pixels: make([]byte, 256), Width: 2, Height: 2, Format: format, BytesPerRow: 256,
		}
		if capture.Image() != nil {
			t.Fatalf("%s produced an image; only 8-bit RGBA can be one", formatName(format))
		}
	}
	words := captureRefusal(ErrCaptureUnsupported{Format: FormatDepth32F}, 1, 1)
	var unavailable mcp.Unavailable
	if !errors.As(words, &unavailable) {
		t.Fatalf("depth refusal = %T, want words an agent can act on", words)
	}
	if !strings.Contains(unavailable.Reason, "depth") {
		t.Fatalf("depth refusal = %q, want it to name what was refused", unavailable.Reason)
	}
}

func TestABurstWritesNumberedStillsAndReportsTheOrdinals(t *testing.T) {
	rig := newCaptureRig(t)
	directory := t.TempDir()

	response, err := rig.runCapture(CaptureRequest{
		Path: filepath.Join(directory, "frame-%04d.png"), Amount: 3, Interval: 2,
	})
	if err != nil {
		t.Fatalf("burst: %v", err)
	}
	if !slices.Equal(response.Indices, []int{0, 1, 2}) {
		t.Fatalf("indices = %v, want every ordinal written", response.Indices)
	}
	for _, ordinal := range response.Indices {
		name := filepath.Join(directory, "frame-000"+string(rune('0'+ordinal))+".png")
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("still %d was reported but not written: %v", ordinal, err)
		}
	}
	if response.Path != filepath.Join(directory, "frame-0000.png") {
		t.Fatalf("response path = %q, want the first still actually written", response.Path)
	}
}

func TestABurstTruncatesRatherThanFailing(t *testing.T) {
	rig := newCaptureRig(t)
	var taken atomic.Int64
	rig.backend.captureResult = func(GpuCaptureDesc) GpuCapture {
		if taken.Add(1) > 2 {
			return GpuCapture{Err: ErrCaptureNoTarget{}}
		}
		return paddedCapture(4, 2, func(int, int) color.NRGBA { return color.NRGBA{A: 255} })
	}
	directory := t.TempDir()

	response, err := rig.runCapture(CaptureRequest{
		Path: filepath.Join(directory, "burst-%04d.png"), Amount: 5,
	})
	if err != nil {
		t.Fatalf("a short burst is a success, got %v", err)
	}
	if !slices.Equal(response.Indices, []int{0, 1}) {
		t.Fatalf("indices = %v, want the two stills that landed", response.Indices)
	}
}

func TestACaptureThatWritesNothingIsAnError(t *testing.T) {
	rig := newCaptureRig(t)
	rig.backend.captureResult = func(GpuCaptureDesc) GpuCapture {
		return GpuCapture{Err: ErrCaptureNoTarget{}}
	}

	_, err := rig.runCapture(CaptureRequest{Path: filepath.Join(t.TempDir(), "none.png")})
	var unavailable mcp.Unavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("zero frames = %v, want words rather than a short success", err)
	}
}

func TestThePathIsCheckedBeforeAFrameIsSpent(t *testing.T) {
	absolute := t.TempDir()
	cases := []struct {
		name    string
		request CaptureRequest
	}{
		{"empty", CaptureRequest{}},
		{"relative", CaptureRequest{Path: filepath.Join("shots", "a.png")}},
		{"not a png", CaptureRequest{Path: filepath.Join(absolute, "a.jpg")}},
		{"wrong verb", CaptureRequest{Path: filepath.Join(absolute, "a-%s.png"), Amount: 2}},
		{"two verbs", CaptureRequest{Path: filepath.Join(absolute, "a-%d-%d.png"), Amount: 2}},
		{"burst with no verb", CaptureRequest{Path: filepath.Join(absolute, "a.png"), Amount: 2}},
		{"too many stills", CaptureRequest{Path: filepath.Join(absolute, "a-%04d.png"), Amount: 61}},
		{"too long a span", CaptureRequest{
			Path: filepath.Join(absolute, "a-%04d.png"), Amount: 60, Interval: 20,
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rig := newCaptureRig(t)
			_, err := captureScreen(rig.k, testCase.request)
			var unavailable mcp.Unavailable
			if !errors.As(err, &unavailable) {
				t.Fatalf("%v was accepted, got %v", testCase.request, err)
			}
			rig.frame("screen")
			rig.frame("screen")
			if len(rig.backend.captureDescs) != 0 {
				t.Fatal("a refused request still cost the engine a frame")
			}
		})
	}
}

func TestASingleCaptureUnderPauseCostsNoTick(t *testing.T) {
	rig := newCaptureRig(t)
	rig.record("frozen")
	rig.tick()
	rig.clock.paused.Store(true)

	pixels := func(GpuCaptureDesc) GpuCapture {
		return paddedCapture(3, 2, func(x, y int) color.NRGBA {
			return color.NRGBA{R: uint8(x * 30), G: uint8(y * 30), A: 255}
		})
	}
	rig.backend.captureResult = pixels
	directory := t.TempDir()

	first := rig.pausedCapture(CaptureRequest{Path: filepath.Join(directory, "a.png")})
	second := rig.pausedCapture(CaptureRequest{Path: filepath.Join(directory, "b.png")})
	if !slices.Equal(first.Indices, []int{0}) || !slices.Equal(second.Indices, []int{0}) {
		t.Fatalf("paused captures wrote %v and %v, want one still each", first.Indices, second.Indices)
	}
	a, err := os.ReadFile(filepath.Join(directory, "a.png"))
	if err != nil {
		t.Fatalf("read the first paused capture: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(directory, "b.png"))
	if err != nil {
		t.Fatalf("read the second paused capture: %v", err)
	}
	if !slices.Equal(a, b) {
		t.Fatal("two captures under one pause differ, and nothing moved between them")
	}
}

// pausedCapture runs one capture while driving renders only, which is what a
// paused engine does: no tick can begin, and the capture is served from the
// next render.
func (r *captureRig) pausedCapture(request CaptureRequest) CaptureResponse {
	r.t.Helper()
	type answer struct {
		response CaptureResponse
		err      error
	}
	done := make(chan answer, 1)
	go func() {
		response, err := captureScreen(r.k, request)
		done <- answer{response, err}
	}()
	expiry := time.After(10 * time.Second)
	for {
		select {
		case got := <-done:
			if got.err != nil {
				r.t.Fatalf("paused capture: %v", got.err)
			}
			return got.response
		case <-expiry:
			r.t.Fatal("the paused capture never answered")
		default:
		}
		r.render()
		time.Sleep(time.Millisecond)
	}
}

func TestABurstUnderPauseIsRefusedInWords(t *testing.T) {
	rig := newCaptureRig(t)
	rig.clock.paused.Store(true)

	_, err := captureScreen(rig.k, CaptureRequest{
		Path: filepath.Join(t.TempDir(), "burst-%04d.png"), Amount: 4,
	})
	var unavailable mcp.Unavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("a paused burst = %v, want words", err)
	}
	if unavailable.Reason == "" {
		t.Fatal("the refusal says nothing an agent can act on")
	}

	// gfx refuses it on its own terms too, for the callers that are not an agent.
	_, armErr := rig.k.ExecuteCommand[ArmCaptureCmd](ArmCaptureRequest{
		Target: GpuCaptureDesc{Screen: true}, Amount: 4, Paused: true,
	})
	if !errors.Is(armErr, ErrCaptureBurstPaused{}) {
		t.Fatalf("paused burst arm = %v, want it refused", armErr)
	}
}

func TestATextureCaptureDeclaresItsTransition(t *testing.T) {
	var target TextureDescr
	rig := newCaptureRig(t)
	withResourceQueue(t, rig.k, func(resources *ResourceQueue) {
		target = resources.AllocateTexture(64, 64, 1, FormatRGBA8)
	})
	if _, err := rig.k.ExecuteCommand[ArmCaptureCmd](ArmCaptureRequest{
		Target: GpuCaptureDesc{Texture: target.ID()},
	}); err != nil {
		t.Fatalf("arm: %v", err)
	}
	q := recordRaw(t, rig.k)
	q.Pass(PassDescr{
		Target: TextureTarget(target, 0, 0), Depth: DepthNone(), Load: LoadClear, Label: "offscreen",
	})
	q.Draw(triangle(), testMaterial(), MatParam("mvp", m.NewMat4()))
	rig.tick()
	rig.render()

	if len(rig.backend.captureDescs) != 1 {
		t.Fatalf("capture ops = %d, want the texture readback", len(rig.backend.captureDescs))
	}
	want := TextureTransition{
		Texture: target.ID(), From: TextureUsageRenderAttachment, To: TextureUsageCopySrc,
	}
	if _, placed := rig.backend.transitionBefore(want); !placed {
		t.Fatalf("transitions = %v, want the texture moved into CopySrc before the copy",
			rig.backend.transitions)
	}
}
