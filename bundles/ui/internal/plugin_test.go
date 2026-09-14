package internal

import (
	stdcontext "context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/bundles/ui"
	"github.com/dvoyni/cog/bundles/ui/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

type pluginTestVisual struct {
	states   []ui.State
	sawQueue bool
}

func (*pluginTestVisual) DefaultSize(canvas.LookupAccess, any) m.Vec2 {
	return m.Vec2{X: 20, Y: 20}
}

func (visual *pluginTestVisual) Draw(_ canvas.LookupAccess, queue *canvas.OpQueue, state ui.State, _ any) {
	visual.sawQueue = queue != nil
	visual.states = append(visual.states, state)
	queue.FillRect(state.Layer, state.Rect, canvas.ShapeDraw{Material: state.Materials.Sprite})
}

type pluginTestBuildHandler kernel.Subscription[app.UpdateEvent]
type pluginTestObserveHandler kernel.Subscription[app.UpdateEvent]

type pluginTestConsumer struct {
	visual       *pluginTestVisual
	tick         int
	left, top    float32
	observed     [][]ui.Interaction
	frameLengths []int
}

func (*pluginTestConsumer) Name() kernel.PluginName { return "ui-test-consumer" }

func (*pluginTestConsumer) Dependencies() []kernel.PluginName { return []kernel.PluginName{ui.Name} }

func (consumer *pluginTestConsumer) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[pluginTestBuildHandler](consumer.build).
		Before[ui.ProcessOnUpdate]()
	registrar.Subscribe[pluginTestObserveHandler](consumer.observe).
		After[ui.ProcessOnUpdate]().
		Before[canvas.FlushOnUpdate]()
	return nil
}
func (consumer *pluginTestConsumer) build() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var frameResource kernel.Write[*ui.Frame]
	return func(access kernel.ResourceAccess) {
			frameResource = access.GetWrite[*ui.Frame]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			frame := frameResource.Get()
			if consumer.tick == 0 {
				frame.Add(10, ui.NewElement().
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
		storage.Name: storage.Config{},
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(
		storageplugin.New(), permanentAdapter{},
		inputplugin.New(),
		gfxplugin.New(),
		backendAdapter{&detachedBackend{}},
		canvasplugin.New(),
		New(),
		consumer,
	)
	go engine.Run(runContext)
	<-engine.Ready()
	k := engine.Executioner()

	k.ExecuteCommand[gfx.SetDesiredViewportCmd](gfx.SetDesiredViewportRequest{
		Mode: gfx.ViewportFit, Width: 100, Height: 80,
	})
	k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 200, Height: 160, FramebufferWidth: 400, FramebufferHeight: 320,
	})
	k.ExecuteCommand[input.ApplyCmd](input.ApplyRequest{Changes: []input.Change{
		input.PointerChange(input.Pos{X: 100, Y: 80}),
		input.KeyChange(input.KeyMouseLeft, 0, true),
	}})
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()

	assertInteractions(t, consumer.observed[0], []ui.Interaction{
		{ID: "button", Kind: ui.InteractionDown, Button: 0},
		{ID: "button", Kind: ui.InteractionIn, Button: -1},
		{ID: "button", Kind: ui.InteractionHover, Button: -1},
	})
}

func (consumer *pluginTestConsumer) observe() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var frameResource kernel.Read[*ui.Frame]
	var interactionsResource kernel.Read[*ui.Interactions]
	return func(access kernel.ResourceAccess) {
			frameResource = access.GetRead[*ui.Frame]()
			interactionsResource = access.GetRead[*ui.Interactions]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			frame := frameResource.Get()
			interactions := interactionsResource.Get()
			values := make([]ui.Interaction, 0)
			for interaction := range interactions.All() {
				values = append(values, interaction)
			}
			consumer.observed = append(consumer.observed, values)
			consumer.frameLengths = append(consumer.frameLengths, len(types.FrameRoots(frame)))
			return nil
		}
}

func TestPluginProcessesAndClearsEveryUpdate(t *testing.T) {
	runContext, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	visual := &pluginTestVisual{}
	consumer := &pluginTestConsumer{visual: visual}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{},
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(
		storageplugin.New(), permanentAdapter{},
		inputplugin.New(),
		gfxplugin.New(),
		backendAdapter{&detachedBackend{}},
		canvasplugin.New(),
		New(),
		consumer,
	)
	go engine.Run(runContext)
	<-engine.Ready()
	k := engine.Executioner()

	k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
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
	if state.ClipRect != (ui.Rect{Width: 100, Height: 80}) {
		t.Fatalf("visual clip = %+v, want screen", state.ClipRect)
	}
	if !state.Has(ui.VisualHovered | ui.VisualPressed) {
		t.Fatalf("visual state = %+v, want hovered and pressed", state)
	}
	assertInteractions(t, consumer.observed[0], []ui.Interaction{
		{ID: "button", Kind: ui.InteractionDown, Button: 0},
		{ID: "button", Kind: ui.InteractionIn, Button: -1},
		{ID: "button", Kind: ui.InteractionHover, Button: -1},
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
	assertInteractions(t, consumer.observed[1], []ui.Interaction{
		{ID: "button", Kind: ui.InteractionUp, Button: 0},
		{ID: "button", Kind: ui.InteractionOut, Button: -1},
	})
}

// A scripted move, press and release in one call is a complete click. The
// batching rule puts every step with no delay between it and the next into one
// tick, and one tick's down and up on one target is what ui turns into
// InteractionClick — so an agent needs no compound click step.
func TestPluginSeesAScriptedClickAsAClick(t *testing.T) {
	runContext, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	consumer := &pluginTestConsumer{visual: &pluginTestVisual{}}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{},
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(storageplugin.New(), permanentAdapter{}, inputplugin.New(), gfxplugin.New(), backendAdapter{&detachedBackend{}}, canvasplugin.New(), New(), consumer)
	go engine.Run(runContext)
	<-engine.Ready()
	k := engine.Executioner()

	k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 100, Height: 80, FramebufferWidth: 100, FramebufferHeight: 80,
	})
	if _, err := input.Play(k, []input.Action{
		{Do: input.ActionMove, X: 5, Y: 5},
		{Do: input.ActionKeyDown, Key: input.KeyMouseLeft},
		{Do: input.ActionKeyUp, Key: input.KeyMouseLeft},
	}); err != nil {
		t.Fatalf("play: %v", err)
	}
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()

	if !hasInteraction(consumer.observed[0], "button", ui.InteractionClick) {
		t.Fatalf("interactions = %+v, want a click", consumer.observed[0])
	}
	if !hasInteraction(consumer.observed[0], "button", ui.InteractionDown) ||
		!hasInteraction(consumer.observed[0], "button", ui.InteractionUp) {
		t.Fatalf("interactions = %+v, want both edges of the click", consumer.observed[0])
	}
}

// A delay between the press and the release is a held press, and a move
// between them is a drag: the capture follows the pointer off the target, so
// the release still reports Up and reports no click.
func TestPluginSeesAScriptedDragAsADrag(t *testing.T) {
	runContext, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	consumer := &pluginTestConsumer{visual: &pluginTestVisual{}}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{},
	}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(storageplugin.New(), permanentAdapter{}, inputplugin.New(), gfxplugin.New(), backendAdapter{&detachedBackend{}}, canvasplugin.New(), New(), consumer)
	go engine.Run(runContext)
	<-engine.Ready()
	k := engine.Executioner()

	k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 100, Height: 80, FramebufferWidth: 100, FramebufferHeight: 80,
	})
	played := make(chan error, 1)
	go func() {
		_, err := input.Play(k, []input.Action{
			{Do: input.ActionMove, X: 5, Y: 5},
			{Do: input.ActionKeyDown, Key: input.KeyMouseLeft},
			{Do: input.ActionDelay, Ms: 150},
			{Do: input.ActionMove, X: 60, Y: 60},
			{Do: input.ActionKeyUp, Key: input.KeyMouseLeft},
		})
		played <- err
	}()

	// The first batch lands before the wait, so the press is observable while
	// the sequence is still mid-delay.
	deadline := time.Now().Add(2 * time.Second)
	for !scriptedButtonHeld(t, k) {
		if time.Now().After(deadline) {
			t.Fatal("the press never landed")
		}
		time.Sleep(time.Millisecond)
	}
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()

	if err := <-played; err != nil {
		t.Fatalf("play: %v", err)
	}
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()

	if !hasInteraction(consumer.observed[0], "button", ui.InteractionDown) {
		t.Fatalf("the press tick saw %+v, want a down", consumer.observed[0])
	}
	if hasInteraction(consumer.observed[0], "button", ui.InteractionUp) {
		t.Fatalf("the press tick saw %+v; the delay did not hold the button", consumer.observed[0])
	}
	if !hasInteraction(consumer.observed[1], "button", ui.InteractionUp) {
		t.Fatalf("the release tick saw %+v, want an up", consumer.observed[1])
	}
	if hasInteraction(consumer.observed[1], "button", ui.InteractionClick) {
		t.Fatalf("the release tick saw %+v; a drag off the target is not a click",
			consumer.observed[1])
	}
}

// scriptedButtonHeld asks the input seam the way input_state does, so the test
// waits on the contract rather than on a sleep.
func scriptedButtonHeld(t *testing.T, k kernel.Executioner) bool {
	t.Helper()
	seam, err := k.ExecuteCommand[input.StateCmd](input.StateRequest{})
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	return slices.Contains(seam.Down, input.KeyMouseLeft)
}

func assertInteractions(t *testing.T, got, want []ui.Interaction) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interactions = %+v, want %+v", got, want)
	}
}

func hasInteraction(interactions []ui.Interaction, id ui.ID, kind ui.InteractionKind) bool {
	for index := range interactions {
		if interactions[index].ID == id && interactions[index].Kind == kind {
			return true
		}
	}
	return false
}
