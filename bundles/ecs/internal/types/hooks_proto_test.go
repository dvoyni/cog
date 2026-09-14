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

// hookBench is a set of Stores with no kernel around them.
type hookBench struct {
	en        *Entities
	bodies    *Store[body]
	vels      *Store[velocity]
	colliders *Store[collider]
	names     *Store[named]
}

func benchStore[C any](en *Entities, ids uint32) *Store[C] {
	s := NewStore[C](en, ids)
	en.declare(reflect.TypeFor[C](), newComponentClass(s))
	return s
}

func newHookBench(ids uint32) *hookBench {
	en := newEntities(ids)
	en.hookHub()
	return &hookBench{
		en:        en,
		bodies:    benchStore[body](en, ids),
		vels:      benchStore[velocity](en, ids),
		colliders: benchStore[collider](en, ids),
		names:     benchStore[named](en, ids),
	}
}

// spawn is Spawn.New's body without a kernel handle.
func (w *hookBench) spawn(b body, v velocity) Entity {
	e := w.en.alloc()
	w.bodies.set(e, b)
	w.vels.set(e, v)
	return e
}

type seen struct {
	e     Entity
	kinds string
	value float32
}

func collect[T any, K kindSet](h *Hooks[T, K], value func(*T) float32) []seen {
	h.beginRun()
	var out []seen
	for e, hook := range h.All() {
		var k []string
		for _, kind := range []struct {
			on   bool
			name string
		}{{hook.IsSpawned(), "spawned"}, {hook.IsDespawned(), "despawned"}, {hook.IsAdded(), "added"}, {hook.IsRemoved(), "removed"}, {hook.IsChanged(), "changed"}} {
			if kind.on {
				k = append(k, kind.name)
			}
		}
		out = append(out, seen{e, strings.Join(k, "+"), value(&hook.Value)})
	}
	h.endRun()
	return out
}

func bodyX(b *body) float32 { return b.X }

func expect(t *testing.T, what string, got, want []seen) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s:\n got %v\nwant %v", what, got, want)
	}
}

// TestAWindowHoldsOneValuesCarryingRecord is #379's example on one Component:
// added(v0), changed(v1), removed(v1), added(v2), changed(v3) is delivered as
// added+changed(v1), removed(v1), added+changed(v3).
func TestAWindowHoldsOneValuesCarryingRecord(t *testing.T) {
	w := newHookBench(16)
	var all Hooks[body, HookAll]
	var membership Hooks[body, HookAddedRemoved]
	var despawns Hooks[body, HookDespawned]
	w.en.hooks.preparing = 1
	all.bind(w.en)
	all.writer = 1
	membership.bind(w.en)
	despawns.bind(w.en)
	w.en.hooks.preparing = 2
	writer := snapshotFor[body](w.en)
	write := func(e Entity, x float32) {
		row, _ := w.bodies.probe(e)
		writer.take(e, row)
		w.bodies.dense[row].X = x
		writer.finish()
	}

	e := w.en.alloc()
	w.bodies.Set(e, body{X: 0})
	write(e, 1)
	w.bodies.Remove(e)
	w.bodies.Set(e, body{X: 2})
	write(e, 3)
	expect(t, "HookAll, first run", collect(&all, bodyX), []seen{
		{e, "added+changed", 1}, {e, "removed", 1}, {e, "added+changed", 3},
	})
	expect(t, "HookAddedRemoved sees no change, and its addition carries the value at the removal", collect(&membership, bodyX), []seen{
		{e, "added+changed", 1}, {e, "removed", 1}, {e, "added+changed", 3},
	})
	expect(t, "HookDespawned sees nothing", collect(&despawns, bodyX), nil)

	// A standalone change folds to one; the reader's own change is never seen.
	write(e, 11)
	write(e, 12)
	w.en.hooks.preparing = 1
	own := snapshotFor[body](w.en)
	other := w.en.alloc()
	w.bodies.Set(other, body{X: 50})
	membership.beginRun()
	membership.endRun()
	row, _ := w.bodies.probe(other)
	own.take(other, row)
	w.bodies.dense[row].X = 99
	own.finish()
	expect(t, "HookAll, second run", collect(&all, bodyX), []seen{
		{e, "changed", 12}, {other, "added+changed", 99},
	})

	// A despawn carries the last value, once, as removed and despawned.
	w.en.despawn(e)
	expect(t, "HookAll, despawn", collect(&all, bodyX), []seen{{e, "despawned+removed", 12}})
	expect(t, "HookDespawned, despawn", collect(&despawns, bodyX), []seen{{e, "despawned+removed", 12}})
}

// TestASpawnIsAnAdditionOnEveryStoreItCarries.
func TestASpawnIsAnAdditionOnEveryStoreItCarries(t *testing.T) {
	w := newHookBench(16)
	var bodies Hooks[body, HookSpawnedDespawned]
	var vels Hooks[velocity, HookAdded]
	bodies.bind(w.en)
	vels.bind(w.en)
	e := w.spawn(body{X: 1}, velocity{X: 2})
	lone := w.en.alloc()
	w.bodies.Set(lone, body{X: 7})
	w.en.despawn(e)
	w.en.despawn(lone)
	expect(t, "bodies", collect(&bodies, bodyX), []seen{
		{e, "spawned+added+changed", 1}, {e, "despawned+removed", 1}, {lone, "despawned+removed", 7},
	})
	expect(t, "velocities", collect(&vels, func(v *velocity) float32 { return v.X }), []seen{
		{e, "spawned+added+changed", 2},
	})
}

// TestARetainedStringIsReleasedAtTheReset: a removal's copy keeps its string
// alive until the reader's run ends, and no longer.
func TestARetainedStringIsReleasedAtTheReset(t *testing.T) {
	w := newHookBench(16)
	var reader Hooks[named, HookRemoved]
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
		t.Fatal("the string was released while an unread removal still held it")
	default:
	}
	reader.beginRun()
	for _, hook := range reader.All() {
		if !strings.HasPrefix(hook.Value.Name, "a name") {
			t.Fatalf("retained name is %q", hook.Value.Name)
		}
	}
	reader.endRun()
	for range 20 {
		runtime.GC()
		runtime.Gosched()
		select {
		case <-released:
			return
		default:
		}
	}
	t.Fatal("the string outlived the reader's reset")
}
