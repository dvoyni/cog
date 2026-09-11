package input

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
)

// playHarness is an engine with input, a tick source a test owns, and a probe
// watching the seam. The tick source never runs on its own, which is exactly
// what a paused engine is: nothing advances until something steps it.
type playHarness struct {
	t     *testing.T
	k     kernel.Executioner
	probe *playProbe
}

func newPlayHarness(t *testing.T) *playHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	probe := &playProbe{}
	engine := kernel.New(nil).Handler(func(err error) bool {
		// A caller hanging up cancels whatever its dispatch still had in
		// flight, an event publication included. That is the behaviour under
		// test rather than a fault, and it can surface after the test that
		// caused it has returned.
		if errors.Is(err, context.Canceled) {
			return false
		}
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(New(), pausedHost{}, probe)
	go engine.Run(ctx)
	<-engine.Ready()
	return &playHarness{t: t, k: engine.Executioner(), probe: probe}
}

// seam reads the input seam the way input_state does, through the contract
// rather than through internals.
func (h *playHarness) seam() StateResponse {
	h.t.Helper()
	response, err := h.k.ExecuteCommand[StateCmd](StateRequest{})
	if err != nil {
		h.t.Fatalf("state: %v", err)
	}
	return response
}

// step advances the paused engine, the way wgpu_time step does.
func (h *playHarness) step(ticks int) {
	h.t.Helper()
	if _, err := h.k.ExecuteCommand[app.TimeCmd](
		app.TimeRequest{Action: app.TimeStep, Steps: ticks}); err != nil {
		h.t.Fatalf("step: %v", err)
	}
}

func (h *playHarness) play(actions ...Action) StateResponse {
	h.t.Helper()
	response, err := Play(h.k, actions)
	if err != nil {
		h.t.Fatalf("play: %v", err)
	}
	return response
}

// pausedHost is a tick source that is always paused: nothing ticks until a
// step asks for ticks, and a step publishes exactly the ticks it was asked
// for. It is app.TimeCmd's contract with the parts a window would own left
// out, which is enough for the recipe pause exists to make possible.
type pausedHost struct{}

func (pausedHost) Name() kernel.PluginName           { return "test-paused-host" }
func (pausedHost) Dependencies() []kernel.PluginName { return nil }
func (pausedHost) Register(r *kernel.Registrar, _ any) error {
	r.HandleCommand[app.TimeCmd](func() (kernel.Lock, kernel.Execute[app.TimeRequest, app.TimeResponse]) {
		return nil, func(k kernel.Kernel, request app.TimeRequest) (app.TimeResponse, error) {
			if request.Action != app.TimeStep {
				return app.TimeResponse{Paused: true}, nil
			}
			steps := max(request.Steps, 1)
			for range steps {
				k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
			}
			return app.TimeResponse{Paused: true, Stepped: steps}, nil
		}
	})
	return nil
}

// playProbe records what each tick saw and every discrete event published,
// which is how a test asks what the game would have seen rather than what the
// state happens to hold now.
type playProbe struct {
	mu     sync.Mutex
	ticks  []tickSample
	keys   []KeyEvent
	text   []rune
	probed Key
}

type tickSample struct {
	Pressed, JustPressed, JustReleased bool
	Text                               string
}

type playProbeTickHandler kernel.Subscription[app.UpdateEvent]
type playProbeKeyHandler kernel.Subscription[KeyEvent]
type playProbeTextHandler kernel.Subscription[TextEvent]

func (*playProbe) Name() kernel.PluginName           { return "test-probe" }
func (*playProbe) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *playProbe) Register(r *kernel.Registrar, _ any) error {
	r.Subscribe[playProbeTickHandler](p.sample)
	r.Subscribe[playProbeKeyHandler](func() (kernel.Lock, kernel.Observe[KeyEvent]) {
		return nil, func(_ kernel.Kernel, event KeyEvent) error {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.keys = append(p.keys, event)
			return nil
		}
	})
	r.Subscribe[playProbeTextHandler](func() (kernel.Lock, kernel.Observe[TextEvent]) {
		return nil, func(_ kernel.Kernel, event TextEvent) error {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.text = append(p.text, event.Rune)
			return nil
		}
	})
	return nil
}

// sample reads the state the way gameplay does, after input's own First()
// subscriber has rolled the tick's edges.
func (p *playProbe) sample() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var state kernel.Read[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetRead[*State]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			s := state.Get()
			p.mu.Lock()
			defer p.mu.Unlock()
			p.ticks = append(p.ticks, tickSample{
				Pressed:      s.Pressed(p.probed),
				JustPressed:  s.JustPressed(p.probed),
				JustReleased: s.JustReleased(p.probed),
				Text:         string(s.Text()),
			})
			return nil
		}
}

// watch selects the key the per-tick samples report on, and clears what was
// recorded before.
func (p *playProbe) watch(key Key) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probed, p.ticks, p.keys, p.text = key, nil, nil, nil
}

func (p *playProbe) samples() []tickSample {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.ticks)
}

func (p *playProbe) keyEvents() []KeyEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.keys)
}

func (p *playProbe) typed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.text)
}

func held(seam StateResponse, key Key) bool { return slices.Contains(seam.Down, key) }

// byKey indexes published key events by the key they carry. Publication is
// fire-and-forget, so two events from one batch can arrive in either order.
func byKey(events []KeyEvent) map[Key]KeyEvent {
	indexed := make(map[Key]KeyEvent, len(events))
	for _, event := range events {
		indexed[event.Key] = event
	}
	return indexed
}

// waitFor polls until the condition holds, so a test never guesses how long a
// dispatch takes.
func waitFor(t *testing.T, why string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", why)
		}
		time.Sleep(time.Millisecond)
	}
}

// One rule produces every idiom: a delay splits the sequence into another
// dispatch, and nothing else does.
func TestPlan_ADelayIsTheOnlyThingThatSplitsASequence(t *testing.T) {
	click := []Action{
		{Do: ActionMove, X: 10, Y: 20},
		{Do: ActionKeyDown, Key: KeyMouseLeft},
		{Do: ActionKeyUp, Key: KeyMouseLeft},
	}
	if batches := plan(click); len(batches) != 1 || len(batches[0].actions) != 3 {
		t.Errorf("a click planned as %d batches, want one of three steps", len(batches))
	}

	hold := []Action{
		{Do: ActionKeyDown, Key: KeyW},
		{Do: ActionDelay, Ms: 500},
		{Do: ActionKeyUp, Key: KeyW},
	}
	batches := plan(hold)
	if len(batches) != 2 {
		t.Fatalf("a held press planned as %d batches, want 2", len(batches))
	}
	if batches[0].delay != 0 {
		t.Errorf("the first batch waits %s, want nothing", batches[0].delay)
	}
	if batches[1].delay != 500*time.Millisecond {
		t.Errorf("the wait before the second batch is %s, want 500ms", batches[1].delay)
	}

	drag := []Action{
		{Do: ActionMove, X: 1, Y: 1},
		{Do: ActionKeyDown, Key: KeyMouseLeft},
		{Do: ActionDelay, Ms: 100},
		{Do: ActionMove, X: 9, Y: 9},
		{Do: ActionKeyUp, Key: KeyMouseLeft},
	}
	if batches := plan(drag); len(batches) != 2 ||
		len(batches[0].actions) != 2 || len(batches[1].actions) != 2 {
		t.Errorf("a drag planned as %+v, want two batches of two steps", batches)
	}

	if batches := plan(nil); len(batches) != 1 || len(batches[0].actions) != 0 {
		t.Errorf("an empty sequence planned as %d batches, want one empty one", len(batches))
	}
	if batches := plan([]Action{{Do: ActionDelay, Ms: 5}}); len(batches) != 2 {
		t.Errorf("a trailing delay planned as %d batches, want its wait honoured", len(batches))
	}
}

// Move, down and up in one call is a complete click: the down and the up land
// in the same tick, so both edges fire and the key is never observed held.
func TestPlay_AClickIsOneCallAndOneTick(t *testing.T) {
	harness := newPlayHarness(t)
	harness.probe.watch(KeyMouseLeft)

	seam := harness.play(
		Action{Do: ActionMove, X: 40, Y: 30},
		Action{Do: ActionKeyDown, Key: KeyMouseLeft},
		Action{Do: ActionKeyUp, Key: KeyMouseLeft},
	)
	if seam.Pointer != (Pos{X: 40, Y: 30}) {
		t.Errorf("pointer = %+v, want the move", seam.Pointer)
	}
	if held(seam, KeyMouseLeft) {
		t.Error("the button is still down after a click")
	}

	harness.step(1)
	samples := harness.probe.samples()
	if len(samples) != 1 {
		t.Fatalf("%d ticks ran, want 1", len(samples))
	}
	if !samples[0].JustPressed || !samples[0].JustReleased {
		t.Errorf("the tick saw %+v, want both edges of a click", samples[0])
	}
	if samples[0].Pressed {
		t.Error("a same-tick click was observed held, which it never is")
	}
}

// A delay between the down and the up is a held press, and it is held across
// the ticks the wait spans rather than resolved inside one.
func TestPlay_ADelayHoldsTheKeyBetweenBatches(t *testing.T) {
	harness := newPlayHarness(t)
	harness.probe.watch(KeyW)

	played := make(chan error, 1)
	go func() {
		_, err := Play(harness.k, []Action{
			{Do: ActionKeyDown, Key: KeyW},
			{Do: ActionDelay, Ms: 300},
			{Do: ActionKeyUp, Key: KeyW},
		})
		played <- err
	}()
	waitFor(t, "the first batch to land", func() bool { return held(harness.seam(), KeyW) })

	harness.step(2)
	if samples := harness.probe.samples(); len(samples) != 2 ||
		!samples[0].Pressed || !samples[1].Pressed {
		t.Errorf("the ticks inside the delay saw %+v, want the key held through both", samples)
	}

	if err := <-played; err != nil {
		t.Fatalf("play: %v", err)
	}
	if held(harness.seam(), KeyW) {
		t.Error("the key is still down after the sequence released it")
	}
}

// No handler sleeps under the input write lock: the wait is between dispatches,
// so an unrelated write lands promptly while a sequence is mid-delay.
func TestPlay_TheWaitHappensOutsideEveryLock(t *testing.T) {
	harness := newPlayHarness(t)

	played := make(chan error, 1)
	go func() {
		_, err := Play(harness.k, []Action{
			{Do: ActionKeyDown, Key: KeyW},
			{Do: ActionDelay, Ms: 600},
			{Do: ActionKeyUp, Key: KeyW},
		})
		played <- err
	}()
	waitFor(t, "the first batch to land", func() bool { return held(harness.seam(), KeyW) })

	start := time.Now()
	harness.k.ExecuteCommand[ApplyCmd](ApplyRequest{
		Changes: []Change{PointerChange(Pos{X: 3, Y: 4})}})
	if waited := time.Since(start); waited > 200*time.Millisecond {
		t.Errorf("a write waited %s for a sequence's delay; the wait is under a lock", waited)
	}
	if err := <-played; err != nil {
		t.Fatalf("play: %v", err)
	}
}

// A caller that hangs up stops the sequence at the next delay rather than
// running it out — and nothing unwinds what already landed, which is the
// stuck key the caps exist to bound.
func TestPlay_AHangUpStopsTheSequenceAtTheNextDelay(t *testing.T) {
	harness := newPlayHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	played := make(chan error, 1)
	go func() {
		_, err := Play(harness.k.WithContext(ctx), []Action{
			{Do: ActionKeyDown, Key: KeyW},
			{Do: ActionDelay, Ms: 9000},
			{Do: ActionKeyUp, Key: KeyW},
		})
		played <- err
	}()
	waitFor(t, "the first batch to land", func() bool { return held(harness.seam(), KeyW) })
	cancel()

	select {
	case err := <-played:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the sequence ended with %v, want the caller's cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the sequence ran its delay out after the caller hung up")
	}
	if !held(harness.seam(), KeyW) {
		t.Error("something released the key; nothing is undone when a caller disconnects")
	}
}

// One batch folds under one lock hold, so a human's mouse move cannot land
// between a move and the step after it — and the answer is read from that same
// hold, so nothing that happens afterwards can rewrite it.
func TestPlay_OneBatchFoldsUnderOneLockHold(t *testing.T) {
	harness := newPlayHarness(t)

	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			default:
			}
			harness.k.ExecuteCommand[ApplyCmd](ApplyRequest{
				Changes: []Change{PointerChange(Pos{X: 999, Y: 999})}})
		}
	}()
	defer func() {
		close(stop)
		<-stopped
	}()

	for range 200 {
		seam := harness.play(
			Action{Do: ActionMove, X: 0, Y: 0},
			Action{Do: ActionMoveBy, Dx: 10, Dy: 10},
			Action{Do: ActionKeyDown, Key: KeyMouseLeft},
			Action{Do: ActionKeyUp, Key: KeyMouseLeft},
		)
		if seam.Pointer != (Pos{X: 10, Y: 10}) {
			t.Fatalf("pointer = %+v; a competing write landed inside the batch", seam.Pointer)
		}
	}
}

// Modifiers are derived from the live down-set after the change is folded, so
// a chord assembled from two steps reports the modifier that is held, and the
// modifier's own press reports itself rather than the state before it.
func TestPlay_ModifiersComeFromTheLiveDownSetAfterTheFold(t *testing.T) {
	harness := newPlayHarness(t)
	harness.probe.watch(KeyS)

	harness.play(
		Action{Do: ActionKeyDown, Key: KeyLeftControl},
		Action{Do: ActionKeyDown, Key: KeyS},
	)
	waitFor(t, "both key events", func() bool { return len(harness.probe.keyEvents()) >= 2 })
	// Publication is fire-and-forget, so the two events are matched by the key
	// they carry rather than by the order they arrive in.
	events := byKey(harness.probe.keyEvents())
	if modifier := events[KeyLeftControl]; !modifier.Mods.Has(ModCtrl) {
		t.Errorf("the modifier's own press published %+v, want it reporting itself held", modifier)
	}
	if chord := events[KeyS]; !chord.Mods.Has(ModCtrl) {
		t.Errorf("the chord published %+v, want ModCtrl on the s", chord)
	}

	seam := harness.seam()
	if !held(seam, KeyLeftControl) || !held(seam, KeyS) {
		t.Errorf("the seam holds %v, want both halves of the chord", seam.Down)
	}

	harness.probe.watch(KeyS)
	harness.play(Action{Do: ActionKeyUp, Key: KeyLeftControl})
	waitFor(t, "the release", func() bool { return len(harness.probe.keyEvents()) >= 1 })
	if release := harness.probe.keyEvents()[0]; release.Mods != 0 {
		t.Errorf("the release published %+v, want no modifier held", release)
	}
}

// Against a paused engine, down-step-up-step holds a key for exactly one tick
// — something a running engine cannot offer.
func TestPlay_UnderPauseAKeyIsHeldForExactlyOneTick(t *testing.T) {
	harness := newPlayHarness(t)
	harness.probe.watch(KeyW)
	if !app.Paused(harness.k) {
		t.Fatal("the harness engine reports itself running")
	}

	harness.play(Action{Do: ActionKeyDown, Key: KeyW})
	harness.step(1)
	harness.play(Action{Do: ActionKeyUp, Key: KeyW})
	harness.step(1)

	samples := harness.probe.samples()
	if len(samples) != 2 {
		t.Fatalf("%d ticks ran, want 2", len(samples))
	}
	if !samples[0].Pressed || !samples[0].JustPressed {
		t.Errorf("the first tick saw %+v, want the key going down", samples[0])
	}
	if samples[1].Pressed || !samples[1].JustReleased {
		t.Errorf("the second tick saw %+v, want the key released", samples[1])
	}
	pressed := 0
	for _, sample := range samples {
		if sample.Pressed {
			pressed++
		}
	}
	if pressed != 1 {
		t.Errorf("the key was held for %d ticks, want exactly 1", pressed)
	}
}

// Under pause a delay separates nothing, because the per-tick edges roll on a
// tick and nothing else. This is documented, not refused: refusing would block
// the recipe above.
func TestPlay_UnderPauseADelaySeparatesNothing(t *testing.T) {
	harness := newPlayHarness(t)
	harness.probe.watch(KeyW)

	harness.play(
		Action{Do: ActionKeyDown, Key: KeyW},
		Action{Do: ActionDelay, Ms: 20},
		Action{Do: ActionKeyUp, Key: KeyW},
	)
	harness.step(1)

	samples := harness.probe.samples()
	if len(samples) != 1 {
		t.Fatalf("%d ticks ran, want 1", len(samples))
	}
	if !samples[0].JustPressed || !samples[0].JustReleased || samples[0].Pressed {
		t.Errorf("the tick saw %+v, want both edges and nothing held", samples[0])
	}
}

// A sequence is refused whole or applied whole: every check runs before the
// first batch, so a refusal leaves the seam exactly as it was.
func TestPlay_RefusesWithNothingApplied(t *testing.T) {
	harness := newPlayHarness(t)
	harness.play(
		Action{Do: ActionMove, X: 7, Y: 8},
		Action{Do: ActionKeyDown, Key: KeyLeftShift},
	)
	before := harness.seam()

	long := make([]Action, maxSteps+1)
	for i := range long {
		long[i] = Action{Do: ActionKeyDown, Key: KeyA}
	}
	for _, one := range []struct {
		name    string
		actions []Action
		says    string
	}{
		{"over the duration cap", []Action{
			{Do: ActionKeyDown, Key: KeyW},
			{Do: ActionDelay, Ms: 6000},
			{Do: ActionDelay, Ms: 6000},
			{Do: ActionKeyUp, Key: KeyW},
		}, "up to 10s"},
		{"over the step cap", long, "up to 256"},
		{"an unknown step", []Action{
			{Do: ActionKeyDown, Key: KeyW},
			{Do: "click"},
		}, "not a step"},
		{"no step kind at all", []Action{{}}, "not a step"},
		{"a delay running backwards", []Action{
			{Do: ActionKeyDown, Key: KeyW},
			{Do: ActionDelay, Ms: -1},
		}, "backwards"},
	} {
		t.Run(one.name, func(t *testing.T) {
			started := time.Now()
			_, err := Play(harness.k, one.actions)
			var reason mcp.Unavailable
			if !errors.As(err, &reason) {
				t.Fatalf("error %v is not an expected outcome a caller can read", err)
			}
			if !strings.Contains(reason.Reason, one.says) {
				t.Errorf("reason %q does not say %q", reason.Reason, one.says)
			}
			if waited := time.Since(started); waited > time.Second {
				t.Errorf("the refusal took %s; every check runs before the first batch", waited)
			}
			if after := harness.seam(); !slices.Equal(after.Down, before.Down) ||
				after.Pointer != before.Pointer {
				t.Errorf("the seam moved to %+v from %+v; a refused sequence applied something",
					after, before)
			}
		})
	}
}

// Releasing a key that is not down is a silent no-op, exactly as the state
// already treats it. Rejecting it would be a half-applied sequence reached by
// an ordinary typo.
func TestPlay_ReleasingAKeyThatIsNotDownIsASilentNoOp(t *testing.T) {
	harness := newPlayHarness(t)
	seam := harness.play(
		Action{Do: ActionKeyDown, Key: KeyA},
		Action{Do: ActionKeyUp, Key: KeyB},
	)
	if !slices.Equal(seam.Down, []Key{KeyA}) {
		t.Errorf("the seam holds %v, want only the key that was pressed", seam.Down)
	}
}

// An empty sequence answers with the seam rather than with nothing, which is
// what makes the response shape the same question every input capability
// answers.
func TestPlay_AnEmptySequenceAnswersWithTheSeam(t *testing.T) {
	harness := newPlayHarness(t)
	harness.play(Action{Do: ActionMove, X: 2, Y: 3}, Action{Do: ActionKeyDown, Key: KeyQ})

	seam, err := Play(harness.k, nil)
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if !slices.Equal(seam.Down, []Key{KeyQ}) || seam.Pointer != (Pos{X: 2, Y: 3}) {
		t.Errorf("the seam is %+v, want what the previous call left", seam)
	}
	if state := harness.seam(); !slices.Equal(state.Down, seam.Down) || state.Pointer != seam.Pointer {
		t.Errorf("state answers %+v where send answered %+v", state, seam)
	}
}

// text enters text and presses no keys. The cost is stated rather than hidden:
// a field built on KeyEvent will not see typed text.
func TestPlay_TextEntersTextAndPressesNoKeys(t *testing.T) {
	harness := newPlayHarness(t)
	harness.probe.watch(KeyH)

	harness.play(Action{Do: ActionText, Text: "hi"})
	harness.step(1)

	samples := harness.probe.samples()
	if len(samples) != 1 || samples[0].Text != "hi" {
		t.Errorf("the tick read %+v, want the text in the order it was typed", samples)
	}
	// Each rune is its own publication, and publications are independent, so
	// the events are counted rather than ordered; the tick above is what
	// asserts the order.
	waitFor(t, "both text events", func() bool { return len(harness.probe.typed()) == 2 })
	if events := harness.probe.keyEvents(); len(events) != 0 {
		t.Errorf("typing published %+v, want no key events", events)
	}
	if seam := harness.seam(); len(seam.Down) != 0 {
		t.Errorf("typing left %v held", seam.Down)
	}
}

// The down-set is sorted, because map iteration is not and a response an agent
// diffs against the last one has to be stable.
func TestPlay_TheDownSetIsSorted(t *testing.T) {
	harness := newPlayHarness(t)
	seam := harness.play(
		Action{Do: ActionKeyDown, Key: KeyZ},
		Action{Do: ActionKeyDown, Key: KeyMouseRight},
		Action{Do: ActionKeyDown, Key: KeyA},
		Action{Do: ActionKeyDown, Key: KeyEnter},
	)
	if !slices.Equal(seam.Down, []Key{KeyMouseRight, KeyA, KeyZ, KeyEnter}) {
		t.Errorf("the down-set is %v, want it sorted by key code", seam.Down)
	}
}
