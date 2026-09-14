package types

// PROTOTYPE, throwaway: proto/ecs-hooks, for https://github.com/dvoyni/cog/issues/380.

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

type named struct {
	Name string
	X, Y float32
}

// hookBench is a set of Stores with no kernel around them, for the recording
// microbenchmarks.
type hookBench struct {
	en        *Entities
	bodies    *Store[body]
	vels      *Store[velocity]
	colliders *Store[collider]
	disableds *Store[disabled]
	names     *Store[named]
}

func benchStore[C any](en *Entities, ids uint32) *Store[C] {
	s := NewStore[C](en, ids)
	en.declare(reflect.TypeFor[C](), newComponentClass(s))
	return s
}

func newHookBench(ids uint32) *hookBench {
	en := newEntities(ids)
	return &hookBench{
		en:        en,
		bodies:    benchStore[body](en, ids),
		vels:      benchStore[velocity](en, ids),
		colliders: benchStore[collider](en, ids),
		disableds: benchStore[disabled](en, ids),
		names:     benchStore[named](en, ids),
	}
}

// spawn is Spawn.New's shape without a kernel handle: a whole act.
func (w *hookBench) spawn(b body, v velocity) Entity {
	e := w.en.alloc()
	w.bodies.set(e, b)
	w.vels.set(e, v)
	if w.en.hooks != nil {
		w.en.hooks.spawned(e)
	}
	return e
}

type windowQ struct {
	Body body
	_    Without[disabled]
	_    Entered
	_    Exited
	_    Changed
}

type seen struct {
	e      Entity
	kinds  string
	values body
}

func collect[Q any](h *Hooks[Q], values func(*Q) body) []seen {
	h.beginRun()
	var out []seen
	for e, hook := range h.All() {
		var k []string
		for _, kind := range []struct {
			on   bool
			name string
		}{{hook.IsSpawned(), "spawned"}, {hook.IsDespawned(), "despawned"}, {hook.IsEntered(), "entered"}, {hook.IsExited(), "exited"}, {hook.IsChanged(), "changed"}} {
			if kind.on {
				k = append(k, kind.name)
			}
		}
		out = append(out, seen{e, strings.Join(k, "+"), values(&hook.Values)})
	}
	h.endRun()
	return out
}

// TestAWindowHoldsOneValuesCarryingRecord is #379's example: entered(v0),
// changed(v1), exited(v1), entered(v2), changed(v3) is delivered as
// entered+changed(v1), exited(v1), entered+changed(v3).
func TestAWindowHoldsOneValuesCarryingRecord(t *testing.T) {
	w := newHookBench(16)
	var reader Hooks[windowQ]
	w.en.hookHub().preparing = 1
	reader.bind(w.en)
	reader.writer = 1
	w.en.hooks.preparing = 2
	writer := snapshotFor[body](w.en)

	write := func(e Entity, x float32) {
		row, _ := w.bodies.probe(e)
		writer.take(e, row)
		w.bodies.dense[row].X = x
		writer.finish()
	}

	e := w.en.alloc()
	w.bodies.Set(e, body{X: 0})      // entered(v0)
	write(e, 1)                      // changed(v1)
	w.disableds.Set(e, disabled{})   // exited(v1)
	w.bodies.dense[0].X = 2          // unrecorded while outside
	w.disableds.Remove(e)            // entered(v2)
	write(e, 3)                      // changed(v3)
	other := w.en.alloc()            // an Entity changed while already inside
	w.bodies.Set(other, body{X: 10}) // before the reader's first run
	first := collect(&reader, func(q *windowQ) body { return q.Body })
	want := []seen{
		{e, "entered+changed", body{X: 1}},
		{e, "exited", body{X: 1}},
		{e, "entered+changed", body{X: 3}},
		{other, "entered+changed", body{X: 10}},
	}
	if fmt.Sprint(first) != fmt.Sprint(want) {
		t.Fatalf("first run:\n got %v\nwant %v", first, want)
	}

	// Next run: a standalone change in the window open when the copy began,
	// folded to one; a change the reader made itself is never seen.
	write(other, 11)
	write(other, 12)
	w.en.hooks.preparing = 1
	own := snapshotFor[body](w.en)
	row, _ := w.bodies.probe(e)
	own.take(e, row)
	w.bodies.dense[row].X = 99
	own.finish()
	second := collect(&reader, func(q *windowQ) body { return q.Body })
	want = []seen{{other, "changed", body{X: 12}}}
	if fmt.Sprint(second) != fmt.Sprint(want) {
		t.Fatalf("second run:\n got %v\nwant %v", second, want)
	}

	// A change to an Entity outside the match is dropped; a despawn out of
	// the match carries its last values.
	outside := w.en.alloc()
	w.disableds.Set(outside, disabled{})
	w.bodies.Set(outside, body{X: 5})
	write(outside, 6)
	w.en.despawn(other)
	third := collect(&reader, func(q *windowQ) body { return q.Body })
	want = []seen{{other, "exited", body{X: 12}}}
	if fmt.Sprint(third) != fmt.Sprint(want) {
		t.Fatalf("third run:\n got %v\nwant %v", third, want)
	}
}

type spawnQ struct {
	Body body
	Vel  velocity
	_    Spawned
	_    Despawned
}

// TestASpawnIsOneAct: a Spawn into the match is one record, and a despawn out
// of it carries values captured before the first Store was emptied.
func TestASpawnIsOneAct(t *testing.T) {
	w := newHookBench(16)
	var reader Hooks[spawnQ]
	reader.bind(w.en)
	e := w.spawn(body{X: 1}, velocity{X: 2})
	lonely := w.en.alloc()
	w.bodies.Set(lonely, body{})
	w.en.despawn(e)
	w.en.despawn(lonely)
	got := collect(&reader, func(q *spawnQ) body { return body{q.Body.X, q.Vel.X} })
	want := []seen{{e, "spawned", body{1, 2}}, {e, "despawned", body{1, 2}}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

type namedQ struct {
	Named named
	_     Exited
}

// TestARetainedStringIsReleasedAtTheReset: an exit's copy keeps its string
// alive until the reader's run ends, and no longer.
func TestARetainedStringIsReleasedAtTheReset(t *testing.T) {
	w := newHookBench(16)
	var reader Hooks[namedQ]
	reader.bind(w.en)
	released := make(chan struct{}, 1)
	e := w.en.alloc()
	func() {
		bytes := []byte("a name nobody else holds")
		name := unsafe.String(&bytes[0], len(bytes))
		runtime.AddCleanup(&bytes[0], func(struct{}) { released <- struct{}{} }, struct{}{})
		w.names.Set(e, named{Name: name})
	}()
	w.names.Remove(e)
	for range 3 {
		runtime.GC()
	}
	select {
	case <-released:
		t.Fatal("the string was released while an unread exit still held it")
	default:
	}
	reader.beginRun()
	for _, hook := range reader.All() {
		if !strings.HasPrefix(hook.Values.Named.Name, "a name") {
			t.Fatalf("retained name is %q", hook.Values.Named.Name)
		}
	}
	reader.endRun()
	for range 3 {
		runtime.GC()
	}
	select {
	case <-released:
	case <-timeoutAfterGC():
		t.Fatal("the string outlived the reader's reset")
	}
}

func timeoutAfterGC() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for range 20 {
			runtime.GC()
			runtime.Gosched()
		}
		close(done)
	}()
	return done
}
