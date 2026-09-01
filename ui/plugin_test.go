package ui

import (
	stdcontext "context"
	"testing"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
)

type pluginTestVisual struct {
	states   []State
	sawQueue bool
}

func (*pluginTestVisual) DefaultSize(canvas.LookupAccess, any) m.Vec2 {
	return m.Vec2{X: 20, Y: 20}
}

func (visual *pluginTestVisual) Draw(_ canvas.LookupAccess, queue *canvas.OpQueue, state State, _ any) {
	visual.sawQueue = queue != nil
	visual.states = append(visual.states, state)
	queue.FillRect(state.Layer, state.Rect, m.Color{})
}

type pluginTestBuildHandler kernel.Subscription[app.UpdateEvent]
type pluginTestObserveHandler kernel.Subscription[app.UpdateEvent]

type pluginTestConsumer struct {
	visual       *pluginTestVisual
	tick         int
	left, top    float32
	observed     [][]Interaction
	frameLengths []int
}

func (*pluginTestConsumer) Name() kernel.PluginName { return "ui-test-consumer" }

func (*pluginTestConsumer) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (consumer *pluginTestConsumer) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[pluginTestBuildHandler](consumer.build).
		Before[UpdateEventHandler]()
	registrar.Subscribe[pluginTestObserveHandler](consumer.observe).
		After[UpdateEventHandler]().
		Before[canvas.UpdateEventHandler]()
	return nil
}
func (consumer *pluginTestConsumer) build() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var frameResource kernel.Write[*Frame]
	return func(access kernel.ResourceAccess) {
			frameResource = access.GetWrite[*Frame]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			frame := frameResource.Get()
			if consumer.tick == 0 {
				frame.Add(10, NewElement().
					ID("button").
					Left(consumer.left).
					Top(consumer.top).
					Layer(3).
					Visual(consumer.visual, nil))
			}
			consumer.tick++
			return nil
		}
}

func TestPluginMapsWindowPointerToLogicalViewport(t *testing.T) {
	runContext, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	consumer := &pluginTestConsumer{visual: &pluginTestVisual{}, left: 40, top: 30}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("ui-pointer-test"),
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(
		storage.New(),
		input.New(),
		gfx.New(),
		canvas.New(),
		New(),
		consumer,
	)
	go engine.Run(runContext)
	<-engine.Ready()
	k := engine.Executioner()

	k.ExecuteCommand[app.SetDesiredViewportCmd](app.SetDesiredViewportRequest{
		Mode: app.ViewportFit, Width: 100, Height: 80,
	})
	k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 200, Height: 160, FramebufferWidth: 400, FramebufferHeight: 320,
	})
	k.ExecuteCommand[input.ApplyCmd](input.ApplyRequest{Changes: []input.Change{
		input.PointerChange(input.Pos{X: 100, Y: 80}),
		input.KeyChange(input.KeyMouseLeft, 0, true),
	}})
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()

	assertInteractions(t, consumer.observed[0], []Interaction{
		{ID: "button", Kind: InteractionDown, Button: 0},
		{ID: "button", Kind: InteractionIn, Button: -1},
		{ID: "button", Kind: InteractionHover, Button: -1},
	})
}

func (consumer *pluginTestConsumer) observe() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var frameResource kernel.Read[*Frame]
	var interactionsResource kernel.Read[*Interactions]
	return func(access kernel.ResourceAccess) {
			frameResource = access.GetRead[*Frame]()
			interactionsResource = access.GetRead[*Interactions]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			frame := frameResource.Get()
			interactions := interactionsResource.Get()
			values := make([]Interaction, 0)
			for interaction := range interactions.All() {
				values = append(values, interaction)
			}
			consumer.observed = append(consumer.observed, values)
			consumer.frameLengths = append(consumer.frameLengths, len(frame.roots))
			return nil
		}
}

func TestPluginProcessesAndClearsEveryUpdate(t *testing.T) {
	runContext, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	visual := &pluginTestVisual{}
	consumer := &pluginTestConsumer{visual: visual}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("ui-test"),
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(
		storage.New(),
		input.New(),
		gfx.New(),
		canvas.New(),
		New(),
		consumer,
	)
	go engine.Run(runContext)
	<-engine.Ready()
	k := engine.Executioner()

	k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 100, Height: 80, FramebufferWidth: 100, FramebufferHeight: 80,
	})
	k.ExecuteCommand[input.ApplyCmd](input.ApplyRequest{Changes: []input.Change{
		input.PointerChange(input.Pos{X: 5, Y: 5}),
		input.KeyChange(input.KeyMouseLeft, 0, true),
	}})
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()

	if !visual.sawQueue || len(visual.states) != 1 {
		t.Fatalf("visual queue = %v, draw count = %d; want true, 1", visual.sawQueue, len(visual.states))
	}
	state := visual.states[0]
	if state.Layer != 13 {
		t.Fatalf("visual layer = %d, want 13", state.Layer)
	}
	if state.ClipRect != (Rect{Width: 100, Height: 80}) {
		t.Fatalf("visual clip = %+v, want screen", state.ClipRect)
	}
	if !state.Has(VisualHovered | VisualPressed) {
		t.Fatalf("visual state = %+v, want hovered and pressed", state)
	}
	assertInteractions(t, consumer.observed[0], []Interaction{
		{ID: "button", Kind: InteractionDown, Button: 0},
		{ID: "button", Kind: InteractionIn, Button: -1},
		{ID: "button", Kind: InteractionHover, Button: -1},
	})
	if consumer.frameLengths[0] != 0 {
		t.Fatalf("frame length after processing = %d, want 0", consumer.frameLengths[0])
	}

	k.ExecuteCommand[input.ApplyCmd](input.ApplyRequest{Changes: []input.Change{
		input.KeyChange(input.KeyMouseLeft, 0, false),
	}})
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()

	if len(visual.states) != 1 {
		t.Fatalf("draw count after empty frame = %d, want 1", len(visual.states))
	}
	assertInteractions(t, consumer.observed[1], []Interaction{
		{ID: "button", Kind: InteractionUp, Button: 0},
		{ID: "button", Kind: InteractionOut, Button: -1},
	})
}

func TestFrameBorrowsDescendantsUntilProcessing(t *testing.T) {
	first := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	replacement := &recordingVisual{defaultSize: m.Vec2{X: 10, Y: 10}}
	children := []Element{NewElement().Visual(first, nil)}
	var frame Frame
	frame.Add(4, NewElement().Width(20).Height(20).Children(children...))
	children[0] = NewElement().Visual(replacement, nil)

	var processor processor
	processor.process(canvas.LookupAccess{}, frame.roots, frame.layers, globalState{Screen: Rect{Width: 20, Height: 20}}, nil)
	if len(first.states) != 0 || len(replacement.states) != 1 {
		t.Fatalf("draw counts = original %d, replacement %d; want 0, 1", len(first.states), len(replacement.states))
	}
}

var interactionSum int

func TestInteractionsIterationDoesNotAllocate(t *testing.T) {
	interactions := Interactions{values: []Interaction{
		{ID: "first", Kind: InteractionHover, Button: -1},
		{ID: "second", Kind: InteractionClick, Button: 0},
	}}
	if found, _ := interactions.Has("second", InteractionClick, 0, false); !found {
		t.Fatal("Has did not find interaction")
	}

	allocations := testing.AllocsPerRun(100, func() {
		total := 0
		for interaction := range interactions.All() {
			total += interaction.Button
		}
		interactionSum = total
	})
	if allocations != 0 {
		t.Fatalf("All allocations = %v, want 0", allocations)
	}
}

func TestInteractionsHasUsesAndConsumesTopmostKind(t *testing.T) {
	interactions := Interactions{values: []Interaction{
		{ID: "bottom", Kind: InteractionClick, Button: 0},
		{ID: "hovered", Kind: InteractionHover, Button: -1},
		{ID: "top", Kind: InteractionClick, Button: 0},
	}}

	if found, _ := interactions.Has("bottom", InteractionClick, 0, true); found {
		t.Fatal("Has found a click below the topmost click")
	}
	if len(interactions.values) != 3 {
		t.Fatalf("failed Has consumed interactions: got %d values, want 3", len(interactions.values))
	}
	if found, _ := interactions.Has("top", InteractionClick, 0, true); !found {
		t.Fatal("Has did not find the topmost click")
	}
	if len(interactions.values) != 1 || interactions.values[0].Kind != InteractionHover {
		t.Fatalf("consumed interactions = %v, want only hover", interactions.values)
	}
}

func TestInteractionsHasReturnsTopmostUserData(t *testing.T) {
	interactions := Interactions{values: []Interaction{
		{ID: "bottom", Kind: InteractionClick, Button: 0},
		{ID: "top", Kind: InteractionClick, Button: 0, userData: "top data"},
	}}

	found, userData := interactions.Has("top", InteractionClick, 0, false)
	if !found || userData != "top data" {
		t.Fatalf("Has result = %v, %q; want true, %q", found, userData, "top data")
	}
}
