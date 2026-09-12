package ecsscene

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/scene"
)

// TestAModelNobodyRegisteredDrawsNothing is the other half of the manifest: a
// hash resolves or it does not, and a Component's unset field is NoHash, which
// means no model at all rather than whatever registered first.
func TestAModelNobodyRegisteredDrawsNothing(t *testing.T) {
	h := newHarness(t, testConfig())
	h.spawn(t, spawnRequest{Count: 1, Model: ecs.HashOf[ModelHash]("barrel")})
	h.spawn(t, spawnRequest{Count: 1, Model: ecs.NoHash})
	h.spawn(t, spawnRequest{Count: 1, Model: crate})

	h.frame(t)

	models := h.modelOps(t)
	if len(models) != 1 || models[0].Path != crateModel {
		t.Fatalf("the frame recorded %d model draws %v, want only the registered one", len(models), models)
	}
	if errs := h.errs.snapshot(); len(errs) != 0 {
		t.Fatalf("an unregistered name was reported: %v", errs)
	}
}

// TestEveryDrawableIsRecordedWhereItStands walks several Entities in one frame,
// because a Query that yields one right answer proves nothing about the fill.
func TestEveryDrawableIsRecordedWhereItStands(t *testing.T) {
	h := newHarness(t, testConfig())
	h.spawn(t, spawnRequest{Count: 4, Model: crate, Step: 10})

	h.frame(t)

	models := h.modelOps(t)
	if len(models) != 4 {
		t.Fatalf("the frame recorded %d model draws, want 4", len(models))
	}
	seen := map[float32]bool{}
	for _, op := range models {
		seen[op.Model.Transform.Position.X] = true
	}
	for _, want := range []float32{0, 10, 20, 30} {
		if !seen[want] {
			t.Errorf("no draw stands at x=%v; recorded %v", want, seen)
		}
	}
}

// TestALayerMaskReachesTheDraw is the whole of what Drawable says beyond which
// model it is, and it is scene's own type: a Component may hold it because it is
// a plain number.
func TestALayerMaskReachesTheDraw(t *testing.T) {
	h := newHarness(t, testConfig())
	e := h.spawn(t, spawnRequest{Count: 1, Model: crate})
	h.setLayers(t, e, scene.Layer(3))

	h.frame(t)

	models := h.modelOps(t)
	if len(models) != 1 || models[0].Layers != scene.Layer(3) {
		t.Fatalf("the draw was recorded on layers %v, want %v", models, scene.Layer(3))
	}
}

// TestTheBindingNeverHandsSceneAPointerIntoAStore is the prohibition that has
// no compiler behind it.
//
// scene.ModelDraw.Transform.Matrix is a *m.Mat4 that scene retains by value
// until the flush — and the flush is a different System, running after the
// recording System's locks are gone, so a matrix pointing into a Component
// Store would be read unlocked. The binding uses the TRS form, and the
// Component it reads has no Matrix field to hand over in the first place.
func TestTheBindingNeverHandsSceneAPointerIntoAStore(t *testing.T) {
	h := newHarness(t, testConfig())
	h.spawn(t, spawnRequest{Count: 3, Model: crate, Step: 1})

	h.frame(t)

	for _, op := range h.modelOps(t) {
		if op.Model.Transform.Matrix != nil {
			t.Fatalf("a recorded draw carries a matrix pointer: %v", op.Model.Transform.Matrix)
		}
	}
	// The Component the transform is read out of cannot hold a pointer at all,
	// which is checked when the type is registered and is restated here because
	// it is the reason the recording path is allowed to be this direct.
	for _, componentType := range []reflect.Type{
		reflect.TypeFor[Transform](), reflect.TypeFor[Drawable](), reflect.TypeFor[Animation](),
	} {
		if err := ecs.PointerFree(componentType); err != nil {
			t.Errorf("%s: %v", componentType, err)
		}
	}
}

// TestAnimationPlaysReachTheDrawThroughTheManifest is the variable-length half:
// a Component holds a fixed-capacity array because scene's own cap is four, the
// System flattens it into scratch, and scene copies the scratch at record time.
func TestAnimationPlaysReachTheDrawThroughTheManifest(t *testing.T) {
	h := newHarness(t, testConfig())
	h.spawn(t, spawnRequest{Count: 1, Model: crate, Animated: true, Animation: Animation{
		Plays: [MaxPlays]Play{
			{Clip: walk, Time: 0.25, Weight: 1, Loop: true},
			// A slot naming a clip nobody registered is dropped, and an unset
			// slot is NoHash and is skipped: the slot count is a property of the
			// data rather than a second field that can disagree with it.
			{Clip: ecs.HashOf[ClipHash]("swim"), Weight: 1},
			{Clip: idle, Weight: 0.5},
		},
	}})

	h.frame(t)

	models := h.modelOps(t)
	if len(models) != 1 {
		t.Fatalf("the frame recorded %d model draws, want 1", len(models))
	}
	plays := models[0].Model.Plays
	want := []scene.ClipPlay{
		{Clip: walkClip, Time: 0.25, Loop: true, Weight: 1},
		{Clip: idleClip, Weight: 0.5},
	}
	if !reflect.DeepEqual(plays, want) {
		t.Fatalf("the draw plays %v, want %v", plays, want)
	}
}

// TestOneScratchServesEveryDrawableInAFrame is what makes the scratch legal.
//
// Scene copies every slice on every descriptor into its own frame arena before
// the recording call returns, so one scratch rebuilt per drawable is not two
// drawables sharing one list. If the copy were an alias, both draws below would
// report the play of whichever was recorded last.
func TestOneScratchServesEveryDrawableInAFrame(t *testing.T) {
	h := newHarness(t, testConfig())
	h.spawn(t, spawnRequest{Count: 1, Model: crate, Animated: true, Animation: Animation{
		Plays: [MaxPlays]Play{{Clip: walk, Weight: 1}},
	}})
	h.spawn(t, spawnRequest{Count: 1, Model: crate, Step: 5, Animated: true, Animation: Animation{
		Plays: [MaxPlays]Play{{Clip: idle, Weight: 1}, {Clip: walk, Weight: 0.25}},
	}})

	h.frame(t)

	byClipCount := map[int][]string{}
	for _, op := range h.modelOps(t) {
		clips := make([]string, 0, len(op.Model.Plays))
		for _, play := range op.Model.Plays {
			clips = append(clips, play.Clip)
		}
		byClipCount[len(clips)] = clips
	}
	if got, want := byClipCount[1], []string{walkClip}; !reflect.DeepEqual(got, want) {
		t.Errorf("the one-play draw plays %v, want %v", got, want)
	}
	if got, want := byClipCount[2], []string{idleClip, walkClip}; !reflect.DeepEqual(got, want) {
		t.Errorf("the two-play draw plays %v, want %v", got, want)
	}
}

// TestAnUnanimatedDrawableIsStillRecorded is why Animation is reached through an
// Accessor rather than named in the Query: a Query matches an Entity having at
// least the Components it names, so naming Animation would drop every
// unanimated drawable out of the walk.
func TestAnUnanimatedDrawableIsStillRecorded(t *testing.T) {
	h := newHarness(t, testConfig())
	h.spawn(t, spawnRequest{Count: 2, Model: crate, Step: 1})
	h.spawn(t, spawnRequest{Count: 1, Model: crate, Step: 1, Animated: true, Animation: Animation{
		Plays: [MaxPlays]Play{{Clip: walk, Weight: 1}},
	}})

	h.frame(t)

	models := h.modelOps(t)
	animated := 0
	for _, op := range models {
		if len(op.Model.Plays) > 0 {
			animated++
		}
	}
	if len(models) != 3 || animated != 1 {
		t.Fatalf("the frame recorded %d draws, %d animated; want 3 and 1", len(models), animated)
	}
}

// TestTheRecordingSystemNeedsNoOrderingVocabulary is the ordering claim.
//
// scene subscribes its flush Last().Before[gfx.UpdateEventHandler](), so a
// recording System that does not ask to be last is in the ordinary phase and
// already runs before it. The binding declares no First, no Last, no Before and
// no After, and the frames above are the behavioural half: a draw recorded in a
// tick is in the recording that same tick's flush published.
func TestTheRecordingSystemNeedsNoOrderingVocabulary(t *testing.T) {
	h := newHarness(t, testConfig())
	description := h.engine.Describe()

	var recorder, flush *kernel.SubscriptionDescription
	for i := range description.Subscriptions {
		switch description.Subscriptions[i].Type {
		case reflect.TypeFor[RecordEventHandler]():
			recorder = &description.Subscriptions[i]
		case reflect.TypeFor[scene.UpdateEventHandler]():
			flush = &description.Subscriptions[i]
		}
	}
	if recorder == nil || flush == nil {
		t.Fatalf("the description lists %d subscriptions and not both halves", len(description.Subscriptions))
	}
	if recorder.Phase != "ordinary" {
		t.Errorf("the recording System is in phase %q, want the ordinary one it never asked to leave",
			recorder.Phase)
	}
	if flush.Phase != "last" {
		t.Errorf("scene's flush is in phase %q, want last: the ordering claim rests on it", flush.Phase)
	}
	// The edge exists and points the right way, and neither side declared it.
	// The recorder's own prerequisites are gfx's first-phase handlers, which
	// every ordinary subscriber inherits from the phase and nobody names.
	if !containsType(flush.DependsOn, reflect.TypeFor[RecordEventHandler]()) {
		t.Errorf("scene's flush does not wait for the recording System; it depends on %v", flush.DependsOn)
	}
	if containsType(recorder.DependsOn, reflect.TypeFor[scene.UpdateEventHandler]()) {
		t.Errorf("the recording System waits for scene's flush, which is the wrong way round")
	}
	if recorder.Event != reflect.TypeFor[app.UpdateEvent]() {
		t.Errorf("the recording System is subscribed to %v", recorder.Event)
	}
}

// TestTheRecordingSystemsLockSetIsItsSignature is the other thing the signature
// is: a declaration. Three Stores, the authority, the Manifest and scene's
// queue, none of them named in a Lock func anywhere.
func TestTheRecordingSystemsLockSetIsItsSignature(t *testing.T) {
	h := newHarness(t, testConfig())
	description := h.engine.Describe()
	for _, sub := range description.Subscriptions {
		if sub.Type != reflect.TypeFor[RecordEventHandler]() {
			continue
		}
		wantReads := []reflect.Type{
			reflect.TypeFor[*ecs.Entities](),
			reflect.TypeFor[*Manifest](),
			reflect.TypeFor[*ecs.Store[Transform]](),
			reflect.TypeFor[*ecs.Store[Drawable]](),
			reflect.TypeFor[*ecs.Store[Animation]](),
		}
		for _, want := range wantReads {
			if !containsType(sub.Reads, want) {
				t.Errorf("the System's read set %v does not name %v", sub.Reads, want)
			}
		}
		if !containsType(sub.Writes, reflect.TypeFor[*scene.OpQueue]()) {
			t.Errorf("the System's write set %v does not name scene's queue", sub.Writes)
		}
		if containsType(sub.Writes, reflect.TypeFor[*ecs.Entities]()) {
			t.Errorf("the System holds the authority for write: recording is not a structural change")
		}
		return
	}
	t.Fatal("the description does not list the recording System")
}

func containsType(types []reflect.Type, want reflect.Type) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// TestTheManifestRefusesACollision is the one thing Register can catch, and it
// is caught at composition rather than drawn wrongly at runtime.
func TestTheManifestRefusesACollision(t *testing.T) {
	var table manifest
	if err := table.fill(DefaultConfig().WithModel("crate", "models/crate.glb")); err != nil {
		t.Fatalf("filling the manifest: %v", err)
	}
	if err := table.fill(DefaultConfig().WithModel("crate", "")); err == nil {
		t.Error("a model naming no path was accepted")
	}
	if _, ok := table.Model(crate); !ok {
		t.Error("the manifest does not resolve the name it registered")
	}
	if text, ok := table.ModelText(crate); !ok || text != crateName {
		t.Errorf("the manifest recovers %q for the crate's hash, want %q", text, crateName)
	}
}

// setLayers writes one Drawable's layer mask through an accessor, which is how
// a test changes a Component it did not spawn.
func (h *harness) setLayers(t testing.TB, e ecs.Entity, layers scene.LayerMask) {
	t.Helper()
	if _, err := h.kernel.ExecuteCommand[layerCmd](layerRequest{Entity: e, Layers: layers}); err != nil {
		t.Fatalf("setting layers: %v", err)
	}
}

type layerCmd kernel.Command[layerRequest, layerResponse]

type layerRequest struct {
	Entity ecs.Entity
	Layers scene.LayerMask
}

type layerResponse struct{}

func layerCmdImpl(world *ecs.Entities) func() (kernel.Lock, kernel.Execute[layerRequest, layerResponse]) {
	return ecs.ToExecute[layerRequest, layerResponse](world, func(
		request layerRequest, drawables *ecs.Set[Drawable],
	) {
		if drawable, ok := drawables.Ref(request.Entity); ok {
			drawable.Layers = request.Layers
		}
	})
}

// despawn retires one Entity, which is the only way a drawable stops drawing:
// the ECS owns no lifecycle hooks and scene keeps no per-entity state, so
// nothing has to be told.
func (h *harness) despawn(t testing.TB, e ecs.Entity) {
	t.Helper()
	if _, err := h.kernel.ExecuteCommand[despawnCmd](despawnRequest{Entity: e}); err != nil {
		t.Fatalf("despawning: %v", err)
	}
}

type despawnCmd kernel.Command[despawnRequest, despawnResponse]

type despawnRequest struct {
	Entity ecs.Entity
}

type despawnResponse struct{}

func despawnCmdImpl(world *ecs.Entities) func() (kernel.Lock, kernel.Execute[despawnRequest, despawnResponse]) {
	return ecs.ToExecute[despawnRequest, despawnResponse](world, func(
		request despawnRequest, world *ecs.WriteableEntities,
	) {
		world.Despawn(request.Entity)
	})
}
