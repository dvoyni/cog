package ui

import (
	stdcontext "context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/mcp"
	"github.com/dvoyni/cog/storage"
)

// ui_layout answers "this is in the wrong place; what did layout actually
// decide", and it answers it by putting what was declared beside what was
// resolved. The properties worth pinning are the ones that would make that
// answer a lie: a tree read after the tick that owns it, an index that stops
// addressing what it named once a filter is on, a missing logical viewport
// that breaks find-the-button-click-the-button only on a HiDPI screen, and an
// app's own userData taking down the frame it was supposed to describe.

// snapshotTestVisual is the visual the snapshot names by Go type. It draws
// nothing: what is asserted here is layout, not output, and what canvas
// received is canvas_draws' question.
type snapshotTestVisual struct{}

func (*snapshotTestVisual) DefaultSize(canvas.LookupAccess, any) m.Vec2 {
	return m.Vec2{X: 10, Y: 10}
}

func (*snapshotTestVisual) Draw(canvas.LookupAccess, *canvas.OpQueue, State, any) {}

// liveUserData reports whether it was marshalled while the tick that declared
// it was still running. That is the whole of the in-tick criterion made
// assertable: the tree is readable during the publication and is borrowed
// storage afterwards, so a marshal that saw the flag down was taken at a
// moment the rest of the element could not be trusted.
type liveUserData struct {
	Name string `json:"name"`
	live *atomic.Bool
	// sawLive is written by MarshalJSON, which runs on the engine's goroutine
	// while the test's goroutine waits on the delivery channel, so the
	// hand-off is the channel and this needs no lock of its own beyond being
	// read after it.
	sawLive *atomic.Bool
}

func (data liveUserData) MarshalJSON() ([]byte, error) {
	data.sawLive.Store(data.live.Load())
	return json.Marshal(struct {
		Name string `json:"name"`
	}{Name: data.Name})
}

// panickingUserData is an app's own payload that takes the frame down if
// nothing guards it. A debug facility that can kill a frame is not one.
type panickingUserData struct{}

func (panickingUserData) MarshalJSON() ([]byte, error) {
	panic("this app's userData marshaller is broken")
}

type layoutBuildHandler kernel.Subscription[app.UpdateEvent]
type layoutLastHandler kernel.Subscription[app.UpdateEvent]

// layoutFixture is the engine around ui that a snapshot test drives by hand.
// It plays two parts a real game has and a ui test otherwise does not: the
// producer, which declares the frame every tick, and the tick source, which
// answers whether the engine is paused and publishes the tick a step owes.
type layoutFixture struct {
	mu       sync.Mutex
	declare  func(*Frame)
	step     func(kernel.Kernel, app.TimeRequest) (app.TimeResponse, error)
	requests []app.TimeRequest

	paused atomic.Bool
	// live is raised by the producer, before ui processes, and lowered again
	// from the Last phase, after every default-phase subscriber - the
	// snapshot's included - has run.
	live atomic.Bool
}

func (*layoutFixture) Name() kernel.PluginName { return "ui-snapshot-test" }

// Dependencies names ui, whose Frame the producer locks, and the packages ui
// itself needs so the fixture may be registered beside them.
func (*layoutFixture) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{Name, canvas.Name, gfx.Name, input.Name, storage.Name}
}

func (f *layoutFixture) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[layoutBuildHandler](f.buildOnUpdate).Before[UpdateEventHandler]()
	registrar.Subscribe[layoutLastHandler](f.endOnUpdate).Last()
	registrar.HandleCommand[app.TimeCmd](f.timeCmdImpl)
	return nil
}

func (f *layoutFixture) buildOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var frame kernel.Write[*Frame]
	return func(access kernel.ResourceAccess) {
			frame = access.GetWrite[*Frame]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			f.live.Store(true)
			f.mu.Lock()
			declare := f.declare
			f.mu.Unlock()
			if declare != nil {
				declare(frame.Get())
			}
			return nil
		}
}

// endOnUpdate lowers the live flag from the Last phase, which every
// default-phase subscriber precedes. The snapshot handler is ordered
// After[UpdateEventHandler] and stays in the default phase, so a marshal that
// saw the flag up ran inside the window the tree is valid in.
func (f *layoutFixture) endOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) error {
		f.live.Store(false)
		return nil
	}
}

func (f *layoutFixture) timeCmdImpl() (kernel.Lock, kernel.Execute[app.TimeRequest, app.TimeResponse]) {
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

func (f *layoutFixture) on(declare func(*Frame)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.declare = declare
}

func (f *layoutFixture) onStep(step func(kernel.Kernel, app.TimeRequest) (app.TimeResponse, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.step = step
}

func (f *layoutFixture) asked() []app.TimeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// layoutRig is a ui engine a snapshot test drives by hand: one tick is an
// app.UpdateEvent, and the viewport is set up so the three coordinate sizes
// are three different numbers.
type layoutRig struct {
	t       *testing.T
	k       kernel.Executioner
	fixture *layoutFixture
}

func newLayoutRig(t *testing.T) *layoutRig {
	t.Helper()
	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	fixture := &layoutFixture{}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("ui-layout-test"),
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(storage.New(), input.New(), gfx.New(), canvas.New(), New(), fixture)
	stopped := make(chan struct{})
	go func() { engine.Run(ctx); close(stopped) }()
	<-engine.Ready()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})

	k := engine.Executioner()
	// A fixed-width policy is what makes the logical viewport differ from the
	// window, so a response reporting only two of the three sizes is visible.
	k.ExecuteCommand[app.SetDesiredViewportCmd](app.SetDesiredViewportRequest{
		Mode: app.ViewportFixedWidth, Size: 400,
	})
	k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	return &layoutRig{t: t, k: k, fixture: fixture}
}

func (r *layoutRig) tick() {
	r.t.Helper()
	r.k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
}

// runLayout calls the capability body on its own goroutine and drives ticks
// until it answers, which is what an agent's call looks like from the engine's
// side. What the producer declares in each tick is whatever the test
// installed.
func (r *layoutRig) runLayout(request LayoutRequest) (LayoutResponse, error) {
	r.t.Helper()
	type answer struct {
		response LayoutResponse
		err      error
	}
	done := make(chan answer, 1)
	go func() {
		response, err := layoutSnapshot(r.k, request)
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

// aSmallTree is the frame most of these tests read. Its indices are its
// depth-first pre-order positions:
//
//	0 root   200x100, vertical, padded
//	1 a      a stretch that a declared height contradicts, on its own layer
//	2 b      horizontal
//	3 c        a declared width
//	4 d        nothing declared at all
//	5 e      nothing declared at all
func aSmallTree(userData any) func(*Frame) {
	return func(frame *Frame) {
		frame.Add(10, NewElement().
			ID("root").
			Width(200).Height(100).
			Padding(10).
			Layout(LayoutVertical).
			Children(
				NewElement().
					ID("a").
					Stretch(1).
					Height(20).
					Layer(3).
					UserData(userData).
					Visual(&snapshotTestVisual{}, nil),
				NewElement().
					ID("b").
					Layout(LayoutHorizontal).
					Children(
						NewElement().ID("c").Width(30),
						NewElement().ID("d"),
					),
				NewElement(),
			))
	}
}

func TestALayoutSnapshotReportsWhatResolvedBesideWhatWasDeclared(t *testing.T) {
	rig := newLayoutRig(t)
	sawLive := &atomic.Bool{}
	rig.fixture.on(aSmallTree(liveUserData{
		Name: "hero", live: &rig.fixture.live, sawLive: sawLive,
	}))

	response, err := rig.runLayout(LayoutRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if response.Count != 6 || len(response.Elements) != 6 {
		t.Fatalf("elements = %d of %d, want the whole six-element tree",
			len(response.Elements), response.Count)
	}

	// Flat, depth-first, with an index and a parent index and nothing else
	// about structure.
	ids := make([]string, 0, len(response.Elements))
	for at, element := range response.Elements {
		ids = append(ids, element.ID)
		if element.Index != at {
			t.Errorf("element %d carries index %d, want its position in flatten order",
				at, element.Index)
		}
	}
	if !slices.Equal(ids, []string{"root", "a", "b", "c", "d", ""}) {
		t.Fatalf("ids = %v, want depth-first pre-order with the id-less element last", ids)
	}
	parents := make([]int, 0, len(response.Elements))
	for _, element := range response.Elements {
		parents = append(parents, element.Parent)
	}
	if !slices.Equal(parents, []int{-1, 0, 0, 2, 2, 0}) {
		t.Fatalf("parents = %v, want the root at -1 and every child at its parent's index",
			parents)
	}

	root := response.Elements[0]
	if root.Rect.Width != 200 || root.Rect.Height != 100 {
		t.Errorf("root rect = %+v, want the 200x100 it declared", root.Rect)
	}
	// ContentRect is the rect less this element's own padding, and is where
	// the children were arranged.
	want := RectView{X: root.Rect.X + 10, Y: root.Rect.Y + 10, Width: 180, Height: 80}
	if root.ContentRect != want {
		t.Errorf("root contentRect = %+v, want %+v", root.ContentRect, want)
	}
	// The clip a root is visible through is the logical viewport, which is the
	// space every rect here is in.
	if (root.ClipRect != RectView{Width: 400, Height: 300}) {
		t.Errorf("root clipRect = %+v, want the 400x300 logical viewport", root.ClipRect)
	}
	if !root.Active {
		t.Error("the root laid out and is reported inactive")
	}
	if root.Declared == nil || root.Declared.Width == nil || root.Declared.Width.Value != 200 {
		t.Fatalf("root declared = %+v, want the width it was written with", root.Declared)
	}
	if root.Declared.Layout != "vertical" {
		t.Errorf("root layout = %q, want the enum named rather than numbered",
			root.Declared.Layout)
	}

	// The pairing this capability exists for: a stretch and a height declared
	// together, beside the rect they actually produced.
	child := response.Elements[1]
	if child.Declared == nil || child.Declared.Stretch == nil || *child.Declared.Stretch != 1 {
		t.Fatalf("a declared = %+v, want the stretch it was written with", child.Declared)
	}
	if child.Declared.Height == nil || child.Declared.Height.Value != 20 {
		t.Fatalf("a declared height = %+v, want the 20 it was written with",
			child.Declared.Height)
	}
	// Declared layer is an offset from the root's base; the resolved one is
	// the canvas layer it records into. Reporting only one of them makes the
	// other unrecoverable.
	if child.Declared.Layer == nil || *child.Declared.Layer != 3 {
		t.Fatalf("a declared layer = %v, want the offset it was written with",
			child.Declared.Layer)
	}
	if child.Layer != 13 {
		t.Errorf("a resolved layer = %d, want its root's base 10 plus the 3 it declared",
			child.Layer)
	}
	if child.Visual != "*ui.snapshotTestVisual" {
		t.Errorf("a visual = %q, want the application's own visual type", child.Visual)
	}
	if string(child.UserData) != `{"name":"hero"}` {
		t.Errorf("a userData = %s, want the payload marshalled", child.UserData)
	}
	// The in-tick criterion. A post-tick read compiles, runs, and returns
	// plausible nonsense for half the fields; this is the half that can say
	// so.
	if !sawLive.Load() {
		t.Error("userData was marshalled outside the tick that declared it, where the " +
			"element it came from is borrowed storage")
	}

	// Draw order is the sequence ui draws in - layers ascending, then
	// declaration order - and is not the index: a is declared second and draws
	// last because it put itself on a higher layer.
	orders := make([]int, 0, len(response.Elements))
	for _, element := range response.Elements {
		if element.DrawOrder == nil {
			t.Fatalf("element %d is active and reports no draw order", element.Index)
		}
		orders = append(orders, *element.DrawOrder)
	}
	if !slices.Equal(orders, []int{0, 5, 1, 2, 3, 4}) {
		t.Errorf("draw orders = %v, want a last because its layer is higher", orders)
	}

	// An element that declared nothing carries no declared block at all, which
	// is what makes the declared side free.
	if response.Elements[4].Declared != nil {
		t.Errorf("a plain element carries %+v, want no declared block",
			response.Elements[4].Declared)
	}

	// The three coordinate sizes, including the logical one whose absence is
	// silent on a 1:1 display and breaks the click flow on anything else.
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

func TestASubtreeFilterIsAContiguousRangeWhoseParentLinksResolve(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.on(aSmallTree(nil))
	subtree := 2

	response, err := rig.runLayout(LayoutRequest{Subtree: &subtree})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if response.Count != 6 || response.Omitted != 3 {
		t.Errorf("count = %d omitted = %d, want the whole tree's size and what was dropped",
			response.Count, response.Omitted)
	}
	indices := make([]int, 0, len(response.Elements))
	for _, element := range response.Elements {
		indices = append(indices, element.Index)
	}
	// Flatten is depth-first pre-order and tracks a subtree end, so containment
	// is readable from indices alone and the filter is a slice.
	if !slices.Equal(indices, []int{2, 3, 4}) {
		t.Fatalf("indices = %v, want the contiguous range b, c, d", indices)
	}
	// Every link but the reported root's resolves inside the slice, and the
	// root keeps its true parent rather than being rewritten to look like one.
	if response.Elements[0].Parent != 0 {
		t.Errorf("the subtree root's parent = %d, want its true index in the whole tree",
			response.Elements[0].Parent)
	}
	for _, element := range response.Elements[1:] {
		if !slices.Contains(indices, element.Parent) {
			t.Errorf("element %d names parent %d, which the slice does not contain",
				element.Index, element.Parent)
		}
	}
	// Source indices are what make a filtered tree safe: the ids still belong
	// to the elements the indices name in an unfiltered snapshot.
	if response.Elements[1].ID != "c" || response.Elements[2].ID != "d" {
		t.Errorf("the slice holds %q and %q, want c and d",
			response.Elements[1].ID, response.Elements[2].ID)
	}
}

func TestAMaxDepthFilterKeepsWholeAncestriesAndSaysWhatItDropped(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.on(aSmallTree(nil))
	depth := 1

	response, err := rig.runLayout(LayoutRequest{MaxDepth: &depth})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	indices := make([]int, 0, len(response.Elements))
	for _, element := range response.Elements {
		indices = append(indices, element.Index)
	}
	if !slices.Equal(indices, []int{0, 1, 2, 5}) {
		t.Fatalf("indices = %v, want the root and its children only", indices)
	}
	if response.Omitted != 2 {
		t.Errorf("omitted = %d, want the two grandchildren it dropped", response.Omitted)
	}
	for _, element := range response.Elements[1:] {
		if !slices.Contains(indices, element.Parent) {
			t.Errorf("element %d names parent %d, which the slice does not contain",
				element.Index, element.Parent)
		}
	}
}

func TestUserDataThatCannotMarshalDegradesToItsTypeNameAndNothingElse(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.on(func(frame *Frame) {
		frame.Add(0, NewElement().ID("before").UserData("plain"),
			NewElement().ID("panics").UserData(panickingUserData{}),
			NewElement().ID("channel").UserData(make(chan int)),
			NewElement().ID("after").UserData(7))
	})

	response, err := rig.runLayout(LayoutRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(response.Elements) != 4 {
		t.Fatalf("elements = %d, want the four roots", len(response.Elements))
	}

	// The guard is per element rather than per tree, so one bad payload does
	// not blank the snapshot around it.
	if string(response.Elements[0].UserData) != `"plain"` {
		t.Errorf("the element before the bad one carries %s, want its own payload",
			response.Elements[0].UserData)
	}
	if string(response.Elements[3].UserData) != `7` {
		t.Errorf("the element after the bad one carries %s, want its own payload",
			response.Elements[3].UserData)
	}
	for _, element := range response.Elements {
		if element.ID == "" {
			t.Error("an element lost its id to a neighbour's marshal")
		}
	}

	// The dummy names the type, which is usually the entire answer an agent
	// wanted, and says why rather than becoming null.
	for _, at := range []int{1, 2} {
		var degraded struct {
			Type   string `json:"$type"`
			Opaque bool   `json:"$opaque"`
			Reason string `json:"$reason"`
		}
		if err := json.Unmarshal(response.Elements[at].UserData, &degraded); err != nil {
			t.Fatalf("element %d userData is not JSON: %v", at, err)
		}
		if !degraded.Opaque || degraded.Reason == "" {
			t.Errorf("element %d degraded to %+v, want it marked opaque with a reason",
				at, degraded)
		}
		if degraded.Type == "" {
			t.Errorf("element %d degraded without naming its type", at)
		}
	}
	if got := userDataTypeName(t, response.Elements[1]); got != "ui.panickingUserData" {
		t.Errorf("the panicking payload named itself %q, want its Go type", got)
	}
	if got := userDataTypeName(t, response.Elements[2]); got != "chan int" {
		t.Errorf("the unmarshallable payload named itself %q, want its Go type", got)
	}

	// The whole document still marshals, which is the claim that matters: a
	// debug facility that can kill a frame is not one, and one that produces
	// an unparseable reply is not much better.
	if _, err := json.Marshal(response); err != nil {
		t.Fatalf("the response does not marshal: %v", err)
	}
}

func userDataTypeName(t *testing.T, element ElementView) string {
	t.Helper()
	var degraded struct {
		Type string `json:"$type"`
	}
	if err := json.Unmarshal(element.UserData, &degraded); err != nil {
		t.Fatalf("userData is not JSON: %v", err)
	}
	return degraded.Type
}

func TestAnElementLayoutDroppedIsReportedInactiveRatherThanLeftOut(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.on(func(frame *Frame) {
		frame.Add(0, NewElement().
			ID("grid").
			Layout(LayoutGrid).
			Columns(1).
			Rows(1).
			Children(NewElement().ID("kept"), NewElement().ID("overflowed")))
	})

	response, err := rig.runLayout(LayoutRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(response.Elements) != 3 {
		t.Fatalf("elements = %d, want the grid and both children", len(response.Elements))
	}
	if !response.Elements[1].Active {
		t.Error("the child the grid had room for is reported inactive")
	}
	overflowed := response.Elements[2]
	if overflowed.Active {
		t.Fatal("the child the grid had no room for is reported active")
	}
	// It is reported rather than omitted, because "the element exists and laid
	// out nowhere" is a different answer from "the element is not there" - and
	// it is the answer to why nothing is on screen.
	if overflowed.ID != "overflowed" {
		t.Errorf("the dropped child is %q, want it named", overflowed.ID)
	}
	if overflowed.DrawOrder != nil {
		t.Errorf("an inactive element reports draw order %d, want none", *overflowed.DrawOrder)
	}
}

func TestALayoutSnapshotDescribesATickThatBeganAfterTheRequest(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.on(func(frame *Frame) { frame.Add(0, NewElement().ID("before")) })
	rig.tick()

	armed, err := rig.k.ExecuteCommand[ArmLayoutCmd](ArmLayoutRequest{})
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	select {
	case <-armed.Done:
		t.Fatal("a snapshot arrived without a tick having begun after the request")
	default:
	}

	rig.fixture.on(func(frame *Frame) { frame.Add(0, NewElement().ID("after")) })
	rig.tick()

	select {
	case snapshot := <-armed.Done:
		if snapshot.Err != nil {
			t.Fatalf("snapshot failed: %v", snapshot.Err)
		}
		if len(snapshot.Layout.Elements) != 1 || snapshot.Layout.Elements[0].ID != "after" {
			t.Fatalf("the snapshot describes %+v, want the tree declared after the request",
				snapshot.Layout.Elements)
		}
	default:
		t.Fatal("the tick after the request produced no snapshot")
	}
}

func TestASecondLayoutSnapshotIsRefusedInWordsWhileOneIsInFlight(t *testing.T) {
	rig := newLayoutRig(t)
	if _, err := rig.k.ExecuteCommand[ArmLayoutCmd](ArmLayoutRequest{}); err != nil {
		t.Fatalf("first arm: %v", err)
	}

	_, err := layoutSnapshot(rig.k, LayoutRequest{})
	var refusal mcp.Unavailable
	if !errors.As(err, &refusal) {
		t.Fatalf("a second snapshot answered %v, want words an agent can act on", err)
	}
	if !strings.Contains(refusal.Reason, "already in flight") {
		t.Errorf("reason = %q, want it to say one is already in flight", refusal.Reason)
	}

	// A capture and the other two snapshots are separate slots. Refusing
	// across kinds would destroy the one thing arming them together is for,
	// and with all three siblings built the whole claim can be asserted rather
	// than half of it.
	if _, err := rig.k.ExecuteCommand[canvas.ArmDrawsCmd](canvas.ArmDrawsRequest{}); err != nil {
		t.Errorf("a draw snapshot was refused while a layout snapshot was in flight: %v", err)
	}
	if _, err := rig.k.ExecuteCommand[gfx.ArmFrameCmd](gfx.ArmFrameRequest{}); err != nil {
		t.Errorf("a frame snapshot was refused while a layout snapshot was in flight: %v", err)
	}
	if _, err := rig.k.ExecuteCommand[gfx.ArmCaptureCmd](gfx.ArmCaptureRequest{
		Target: gfx.GpuCaptureDesc{Screen: true},
	}); err != nil {
		t.Errorf("a capture was refused while a layout snapshot was in flight: %v", err)
	}
}

func TestALayoutSnapshotUnderPausePerformsOneStepAndSaysSo(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.paused.Store(true)
	rig.fixture.on(func(frame *Frame) { frame.Add(0, NewElement().ID("frozen")) })
	rig.fixture.onStep(func(k kernel.Kernel, _ app.TimeRequest) (app.TimeResponse, error) {
		k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
		return app.TimeResponse{Paused: true, Stepped: 1}, nil
	})

	// No tick is driven here: a paused engine runs none of its own, so the
	// step the capability raises is the only one, and the ui frame between
	// ticks is empty rather than stale.
	response, err := layoutSnapshot(rig.k, LayoutRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !response.Stepped {
		t.Error("a paused engine was stepped to produce the snapshot, and the response denies it")
	}
	if len(response.Elements) != 1 || response.Elements[0].ID != "frozen" {
		t.Fatalf("elements = %+v, want the tree the step processed", response.Elements)
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

func TestALayoutSnapshotJoiningAPendingStepSaysThatToo(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.paused.Store(true)
	rig.fixture.on(func(frame *Frame) { frame.Add(0, NewElement().ID("shared")) })
	rig.fixture.onStep(func(k kernel.Kernel, _ app.TimeRequest) (app.TimeResponse, error) {
		k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
		return app.TimeResponse{Paused: true, Stepped: 1, Joined: true}, nil
	})

	response, err := layoutSnapshot(rig.k, LayoutRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !response.Stepped || !response.Joined {
		t.Errorf("stepped=%v joined=%v, want both: the tick happened and it was somebody else's",
			response.Stepped, response.Joined)
	}
}

func TestALayoutSnapshotIsWrittenToThePathTheAgentNames(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.on(aSmallTree(nil))
	path := filepath.Join(t.TempDir(), "nested", "layout.json")

	response, err := rig.runLayout(LayoutRequest{Path: path})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if response.Path != path {
		t.Fatalf("response path = %q, want %q", response.Path, path)
	}
	// The elements are in the file; the reply still says how big the tree was
	// and what coordinate frame to read it in, so the agent knows whether
	// opening it is worth a turn.
	if response.Elements != nil {
		t.Error("the element array was returned inline as well as written, doubling the reply")
	}
	if response.Count != 6 || response.ViewportWidth != 400 {
		t.Errorf("counts = %d elements / viewport %v, want them kept inline",
			response.Count, response.ViewportWidth)
	}

	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the written snapshot: %v", err)
	}
	var written LayoutResponse
	if err := json.Unmarshal(document, &written); err != nil {
		t.Fatalf("the written snapshot is not JSON: %v", err)
	}
	if len(written.Elements) != 6 || written.Elements[0].ID != "root" {
		t.Fatalf("the file holds %+v, want the whole tree in flatten order", written.Elements)
	}
	if written.Path != path {
		t.Errorf("the file names itself %q, want %q", written.Path, path)
	}
}

func TestALayoutSnapshotRefusesARequestItCannotHonour(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.on(aSmallTree(nil))
	negative, deep, missing := -1, -2, 99
	for _, test := range []struct {
		name    string
		request LayoutRequest
		wants   string
	}{
		{"relative path", LayoutRequest{Path: filepath.Join("ui", "one.json")}, "absolute"},
		{"wrong extension", LayoutRequest{Path: filepath.Join(t.TempDir(), "ui.txt")}, ".json"},
		{"negative subtree", LayoutRequest{Subtree: &negative}, "negative"},
		{"negative depth", LayoutRequest{MaxDepth: &deep}, "keeps no element"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := layoutSnapshot(rig.k, test.request)
			var refusal mcp.Unavailable
			if !errors.As(err, &refusal) {
				t.Fatalf("answered %v, want words an agent can act on", err)
			}
			if !strings.Contains(refusal.Reason, test.wants) {
				t.Errorf("reason = %q, want it to mention %q", refusal.Reason, test.wants)
			}
		})
	}

	// A subtree index is the one refusal that cannot be raised before the
	// tick: the tree is declared afresh every tick and nothing outside it
	// knows how large it is. It still comes back as words rather than as an
	// empty array reading as a subtree that laid out nothing.
	t.Run("subtree past the end", func(t *testing.T) {
		_, err := rig.runLayout(LayoutRequest{Subtree: &missing})
		var refusal mcp.Unavailable
		if !errors.As(err, &refusal) {
			t.Fatalf("answered %v, want words an agent can act on", err)
		}
		if !strings.Contains(refusal.Reason, "is not in this tick's tree") {
			t.Errorf("reason = %q, want it to say the index names nothing", refusal.Reason)
		}
	})
}

func TestALayoutSnapshotHoldsNothingThatAliasesTheTree(t *testing.T) {
	rig := newLayoutRig(t)
	rig.fixture.on(aSmallTree("carried"))

	response, err := rig.runLayout(LayoutRequest{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	before := marshalLayout(t, response)

	// processor.nodes is rebuilt from its own backing array every tick and
	// layoutNode.element points into the app's borrowed child storage, which
	// the frame releases at the end of each processUpdate. A view still
	// pointing into either - an id, a declared constraint, a userData - reads
	// as whatever the next frames declared.
	rig.fixture.on(func(frame *Frame) {
		for i := range 20 {
			frame.Add(canvas.Layer(i), NewElement().
				ID(ID("other")).
				Width(float32(i)*3).
				UserData(i).
				Children(NewElement().ID("otherchild").Height(float32(i))))
		}
	})
	for range 3 {
		rig.tick()
	}

	if after := marshalLayout(t, response); after != before {
		t.Fatalf("the snapshot changed after three more ticks:\nbefore %s\nafter  %s",
			before, after)
	}
}

func marshalLayout(t *testing.T, response LayoutResponse) string {
	t.Helper()
	document, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal the snapshot: %v", err)
	}
	return string(document)
}

func TestUILayoutIsOfferedAsOneReadOnlyFunc(t *testing.T) {
	offered := New().Capabilities()
	if len(offered) != 1 {
		t.Fatalf("capabilities = %d, want the one ui implements", len(offered))
	}
	capability := offered[0]
	if err := capability.Err(); err != nil {
		t.Fatalf("%s failed construction: %v", layoutCapabilityName, err)
	}
	if capability.Name() != layoutCapabilityName {
		t.Fatalf("name = %q, want %q", capability.Name(), layoutCapabilityName)
	}
	// ReadOnly in cog's reading means the capability does not change the game.
	// The step it costs under pause is stated in the description, because mcp
	// annotates a tool rather than an argument.
	if !capability.ReadOnly() {
		t.Error("ui_layout changes nothing in the game, and is read-only in cog's reading")
	}
	if capability.Description() != layoutDescription {
		t.Error("the description an agent reads is not the one the spec reproduces")
	}
}
