package proto

import (
	"context"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"

	"protoecs/ecs"
)

// cog#246 asks how a plugin that is not the ECS attaches to the world. It names
// physics and audio, and neither exists yet; scene does, and it is the case the
// answer actually has to fit, because a game wants to draw its entities. So
// this file binds the ECS to the real scene package: the real *scene.OpQueue,
// the real recording call, a real kernel engine, and Components carrying what a
// draw needs.
//
// Nothing here flushes. scene.Plugin depends on gfx and storage and the flush
// is where a GPU would be, while the binding question is entirely on the
// recording side -- so these tests publish a bare *scene.OpQueue resource of
// their own and read back what the frame recorded into it.

// ModelID is how a Component names a model: a dense index into a table the app
// interned at registration. It is the answer to the gap this ticket is really
// about, because scene addresses a model by its path and a Component may not
// hold a string.
type ModelID uint32

// modelTable is that table. It is ordinary app-owned memory threaded through a
// plugin constructor the way cog#244 settled, not an engine structure: the ECS
// has no opinion about it beyond requiring that what lands in the Component is
// pointer-free.
type modelTable struct {
	paths  []string
	byPath map[string]ModelID
}

func newModelTable() *modelTable { return &modelTable{byPath: map[string]ModelID{}} }

// Intern resolves a path to an id once, at registration, so the recording path
// never sees a string it has to hash.
func (t *modelTable) Intern(p string) ModelID {
	if id, ok := t.byPath[p]; ok {
		return id
	}
	id := ModelID(len(t.paths))
	t.paths = append(t.paths, p)
	t.byPath[p] = id
	return id
}

// Path is the reverse, and it is a slice index: the whole of what a frame does
// to turn a Component back into what scene's API takes.
func (t *modelTable) Path(id ModelID) string { return t.paths[id] }

// Placement is a pointer-free transform. scene.Transform is not one -- its
// Matrix field is a *m.Mat4 -- so scene's own placement type cannot itself be a
// Component, which is the shape of the whole gap.
type Placement struct {
	Position m.Vec3
	Rotation m.Quat
	Scale    float32
}

// Drawable says an Entity is drawn, in eight bytes, naming no memory it does
// not own.
type Drawable struct {
	Model  ModelID
	Layers scene.LayerMask
}

// DrawQ reads both: recording writes into scene's queue, not into the world, so
// a recording System takes read locks on everything it draws from.
type DrawQ struct {
	P Placement
	D Drawable
}

type (
	recordSub  kernel.Subscription[app.UpdateEvent]
	consumeSub kernel.Subscription[app.UpdateEvent]
)

// recordDraws is the binding, entire, and it is one signature: a System takes
// everything a cog handler may take. Queries for the Components, ecs.Read and
// ecs.Write for the resources -- the same kernel.Read and kernel.Write a
// handler declares, reached through a System parameter instead of through a
// Lock func -- the kernel itself, and the event.
//
// Nothing here is a binding type. The only thing this does that gameplay would
// not is turn an id back into the path scene's API wants.
func recordDraws(
	q *ecs.Query[DrawQ],
	models *ecs.Read[*modelTable],
	out *ecs.Write[*scene.OpQueue],
	k kernel.Kernel,
	_ app.UpdateEvent,
) {
	table, queue := models.Get(), out.Get()
	if table == nil || queue == nil {
		k.ReportError(errors.New("recordDraws: a resource was not initialised"))
		return
	}
	for _, it := range q.All() {
		queue.Model(it.D.Layers, table.Path(it.D.Model), scene.ModelDraw{
			Transform: scene.Transform{
				Position: it.P.Position,
				Rotation: it.P.Rotation,
				Scale:    it.P.Scale,
			},
		})
	}
}

// consumeFrame stands in for scene's own flush, which is subscribed Last(). It
// closes the frame: the queue's slices go back to length zero and keep their
// capacity, which is what makes a steady frame allocate nothing. It records the
// count first so a test can see what the frame recorded.
func consumeFrame(seen *int) any {
	return func(out *ecs.Write[*scene.OpQueue], _ app.UpdateEvent) {
		queue := out.Get()
		*seen = queue.OpCount()
		queue.Reset()
	}
}

type drawWorld struct {
	en         *ecs.Entities
	placements *ecs.Store[Placement]
	drawables  *ecs.Store[Drawable]
	queue      *scene.OpQueue
}

type drawPlug struct {
	w     *drawWorld
	table *modelTable
	n     int
	seen  *int
}

func (drawPlug) Name() kernel.PluginName           { return "draw" }
func (drawPlug) Dependencies() []kernel.PluginName { return nil }

func (p drawPlug) Register(r *kernel.Registrar, _ any) error {
	ids := uint32(p.n + 16)
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	// scene.Plugin publishes this resource itself; a bare one stands in because
	// nothing here flushes, and it is the same type either way.
	queue := new(scene.OpQueue)
	r.InitResource[*scene.OpQueue](queue)
	// The model table is an ordinary resource too, so the interning it did at
	// registration is visible in every recording System's lock set rather than
	// hidden in a closure.
	r.InitResource[*modelTable](p.table)
	placements := ecs.RegisterComponent[Placement](r, en, ids)
	drawables := ecs.RegisterComponent[Drawable](r, en, ids)
	p.w.en, p.w.placements, p.w.drawables, p.w.queue = en, placements, drawables, queue

	crate := p.table.Intern("models/crate.glb")
	for i := range p.n {
		e := en.Alloc()
		placements.Add(e, Placement{Position: m.Vec3{X: float32(i)}, Scale: 1})
		drawables.Add(e, Drawable{Model: crate, Layers: scene.LayersAll})
	}

	r.Subscribe[recordSub, app.UpdateEvent](
		ecs.ToHandler[app.UpdateEvent](en, recordDraws))
	// Ordering is cog's own and needs no ECS vocabulary: scene's real flush is
	// subscribed Last().Before[gfx.UpdateEventHandler](), so a recording System
	// that does not ask to be last already runs before it.
	r.Subscribe[consumeSub, app.UpdateEvent](
		ecs.ToHandler[app.UpdateEvent](en, consumeFrame(p.seen))).Last()
	return nil
}

func startDraw(tb testing.TB, n int) (*kernel.Engine, *drawWorld, *int) {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	w, seen := &drawWorld{}, new(int)
	e := kernel.New(nil).
		Handler(func(err error) bool { tb.Errorf("kernel error: %v", err); return true }).
		WithPlugins(drawPlug{w, newModelTable(), n, seen})
	go e.Run(ctx)
	<-e.Ready()
	return e, w, seen
}

// The binding works, and it is nothing but a System with a resource in its
// signature: no binding type, no adapter, no registration call of the ECS's
// own. One tick, and every Entity carrying a Drawable is a recorded scene draw.
func TestASystemRecordsSceneDrawsFromComponents(t *testing.T) {
	const n = 64
	e, _, seen := startDraw(t, n)
	e.Executioner().PublishEvent(tick).Wait()
	if *seen != n {
		t.Fatalf("recorded %d scene ops, want %d", *seen, n)
	}
	t.Logf("%d Entities became %d recorded scene draws in one tick", n, *seen)
}

// The frame repeats without drift: the queue's arenas keep their capacity
// across a Reset, so the second tick records exactly what the first did.
func TestRecordingRepeatsAcrossFrames(t *testing.T) {
	const n = 32
	e, _, seen := startDraw(t, n)
	for frame := range 5 {
		e.Executioner().PublishEvent(tick).Wait()
		if *seen != n {
			t.Fatalf("frame %d recorded %d ops, want %d", frame, *seen, n)
		}
	}
}

// The gap, made machine-checkable. scene's recording vocabulary was designed
// for a caller that owns its own memory, so almost none of it can be a
// Component: this reports exactly which parts, and why.
//
// MeshRef is the exception worth noticing. It is already a dense id and a
// generation, so a mesh can be named from a Component today -- which is the
// shape a model has yet to be given.
func TestScenesRecordingVocabularyIsNotComponentSafe(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		safe bool
	}{
		{"scene.Transform", reflect.TypeFor[scene.Transform](), false},
		{"scene.ModelDraw", reflect.TypeFor[scene.ModelDraw](), false},
		{"scene.MeshDraw", reflect.TypeFor[scene.MeshDraw](), false},
		{"scene.ClipPlay", reflect.TypeFor[scene.ClipPlay](), false},
		{"scene.Material", reflect.TypeFor[scene.Material](), false},
		{"scene.ModelRef", reflect.TypeFor[scene.ModelRef](), false},
		{"scene.MeshRef", reflect.TypeFor[scene.MeshRef](), true},
		{"scene.LayerMask", reflect.TypeFor[scene.LayerMask](), true},
		{"scene.CameraID", reflect.TypeFor[scene.CameraID](), true},
		{"Placement", reflect.TypeFor[Placement](), true},
		{"Drawable", reflect.TypeFor[Drawable](), true},
	}
	for _, c := range cases {
		err := ecs.PointerFree(c.typ)
		if c.safe && err != nil {
			t.Errorf("%s should be a legal Component: %v", c.name, err)
			continue
		}
		if !c.safe && err == nil {
			t.Errorf("%s passed the Component check; expected it to fail", c.name)
			continue
		}
		if err != nil {
			t.Logf("%-18s rejected: %v", c.name, err)
		} else {
			t.Logf("%-18s legal Component", c.name)
		}
	}
}

// The enforcement point, and the error a user actually gets: naming a model by
// its path is rejected where the Component is registered, not where a draw is
// recorded, and not silently at the first copy.
//
// It reaches the user as an ordinary kernel error rather than as a crash: the
// check panics, like every other registration-time rejection in this
// prototype, and cog turns a panic in Register into a reported error.
func TestRegisteringAStringBearingComponentIsRejected(t *testing.T) {
	_, errs := compose(t, stringPlug{})
	if len(errs) == 0 {
		t.Fatalf("registering a Component with a string field was accepted")
	}
	joined := errors.Join(errs...)
	if !strings.Contains(joined.Error(), "pointer-free") {
		t.Fatalf("want the pointer-free rejection, got %v", joined)
	}
	t.Logf("the error a user sees: %v", joined)
}

type stringPlug struct{}

func (stringPlug) Name() kernel.PluginName           { return "stringly" }
func (stringPlug) Dependencies() []kernel.PluginName { return nil }
func (p stringPlug) Register(r *kernel.Registrar, _ any) error {
	en := ecs.NewEntities(16)
	r.InitResource[*ecs.Entities](en)
	ecs.RegisterComponent[PathedDrawable](r, en, 16)
	return nil
}

// PathedDrawable is the Component an author reaches for first: it names the
// model the way scene's API does. It cannot exist.
type PathedDrawable struct {
	Path   string
	Layers scene.LayerMask
}

// What the binding costs per frame, and how it scales. The bar cog#180 sets is
// time and its scaling with entity count, not allocs/op alone.
func BenchmarkRecordDrawsFromComponents(b *testing.B) {
	// entities=0 is the baseline: the same two Systems, the same publication and
	// the same Wait, with nothing to record. Whatever it costs is cog's own
	// per-tick price, and what the other rows add over it is the binding.
	for _, n := range []int{0, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("entities=%d", n), func(b *testing.B) {
			e, _, seen := startDraw(b, n)
			ex := e.Executioner()
			// Warm the queue's arenas: the first frame grows them, and every
			// frame after it is the steady one the bar is about.
			for range 4 {
				ex.PublishEvent(tick).Wait()
			}
			if *seen != n {
				b.Fatalf("recorded %d ops, want %d", *seen, n)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ex.PublishEvent(tick).Wait()
			}
		})
	}
}

// What naming a model by path costs against naming it by id, which is the
// number that says whether scene should gain an interned handle of its own.
//
// The path side is a replica of what scene does per recorded model draw every
// frame -- modelKey (scene/modelquery.go) then the models map (scene/
// modeltable.go, in requestModel) -- because both are unexported. It is a
// replica, not a measurement of scene, and it is labelled as one.
func BenchmarkNamingAModel(b *testing.B) {
	const n = 1000
	table := newModelTable()
	ids := make([]ModelID, n)
	paths := make([]string, n)
	models := map[string]*int{}
	for i := range n {
		p := fmt.Sprintf("models/props/crate_%03d.glb", i%64)
		paths[i] = p
		ids[i] = table.Intern(p)
		v := i
		models[p] = &v
	}

	b.Run("by-id", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for i := range n {
				sink = table.Path(ids[i])
			}
		}
	})
	b.Run("by-path", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for i := range n {
				key, ok := modelKeyReplica(paths[i])
				if ok {
					entrySink = models[key]
				}
			}
		}
	})
	// The two halves of by-path, so the recommendation lands on whichever one
	// actually costs: normalising the string could be cached, but the map hit
	// is inherent to addressing a model by name.
	b.Run("by-path/normalise-only", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for i := range n {
				sink, _ = modelKeyReplica(paths[i])
			}
		}
	})
	b.Run("by-path/map-only", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for i := range n {
				entrySink = models[paths[i]]
			}
		}
	})
}

var (
	sink      string
	entrySink *int
)

// modelKeyReplica reproduces scene's validateResourcePath and modelKey, which
// are unexported. It is here to price them, not to stand in for them.
func modelKeyReplica(p string) (string, bool) {
	if p == "" || strings.ContainsRune(p, 0) {
		return p, false
	}
	cleaned := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if cleaned == "" || cleaned == "." {
		return p, false
	}
	if strings.HasPrefix(cleaned, "/") {
		return p, false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return p, false
	}
	return cleaned, true
}
