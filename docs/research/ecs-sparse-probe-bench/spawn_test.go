package sbench

import (
	"context"
	"testing"
	"unsafe"
)

// cog#240 Q10, second form: Spawn is not a Command but a handle declared in the
// System's signature, Spawn[struct{Body; Collider}], called as
// spawn.New(bundle). Its Lock emits write{*Entities} plus write{*Store[F]} for
// each field F of the bundle — the same derivation cog#238 already does for a
// Query's fields, run once at registration.
//
// The per-call cost is then a cached closure per field, not reflection. This
// measures that scatter against a hand-written spawn, the mirror of cog#238's
// query-fill measurement (1.37x a hand loop at two Components).

// alloc is the Entities side: take an index, stamp the generation.
func (n *entities) alloc() Entity {
	var idx uint32
	if len(n.free) > 0 {
		idx = n.free[len(n.free)-1]
		n.free = n.free[:len(n.free)-1]
	} else {
		idx = uint32(n.next)
		n.next++
	}
	n.gen[idx]++
	return mkEntity(idx, n.gen[idx])
}

// fieldWriter copies one field of the bundle into the Store that owns it. The
// typed Store is captured at registration, so nothing is reflected per call.
type fieldWriter func(e Entity, bundle unsafe.Pointer)

func writerFor[T any](s *storeB[T], off uintptr) fieldWriter {
	return func(e Entity, bundle unsafe.Pointer) {
		s.add(e, *(*T)(unsafe.Add(bundle, off)))
	}
}

type spawner[B any] struct {
	ents    *entities
	writers []fieldWriter
	scratch *B // bound once at registration; see New vs NewEscaping
}

// NewEscaping is the obvious spelling, and it allocates: &v hands the address
// of a parameter to an opaque func value, which the compiler cannot prove is
// not retained, so the bundle escapes to the heap — one allocation of exactly
// sizeof(B) per spawn.
func (sp *spawner[B]) NewEscaping(v B) Entity {
	e := sp.ents.alloc()
	p := unsafe.Pointer(&v)
	for _, w := range sp.writers {
		w(e, p)
	}
	return e
}

// New copies the bundle into a buffer bound at registration instead, so nothing
// per-call escapes. Safe without synchronisation because the handler holding
// this spawner holds write{*Entities}, which excludes every other handler.
func (sp *spawner[B]) New(v B) Entity {
	e := sp.ents.alloc()
	*sp.scratch = v
	p := unsafe.Pointer(sp.scratch)
	for _, w := range sp.writers {
		w(e, p)
	}
	return e
}

// --- arity 2 --------------------------------------------------------------

type bundle2 struct {
	Body     Body
	Collider Collider
}

func setupSpawn2(space uint32) (*spawner[bundle2], *storeB[Body], *storeB[Collider]) {
	ents := newEntities(space)
	bs, cs := newB[Body](space), newB[Collider](space)
	sp := &spawner[bundle2]{ents: ents, scratch: new(bundle2), writers: []fieldWriter{
		writerFor(bs, unsafe.Offsetof(bundle2{}.Body)),
		writerFor(cs, unsafe.Offsetof(bundle2{}.Collider)),
	}}
	return sp, bs, cs
}

func BenchmarkSpawnBundle_Closures2(b *testing.B) {
	const space = 1 << 16
	sp, bs, cs := setupSpawn2(space)
	v := bundle2{Body: Body{X: 1}, Collider: Collider{R: 2}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		e := sp.New(v)
		bs.remove(e)
		cs.remove(e)
		sp.ents.free = append(sp.ents.free, e.idx())
	}
}

func BenchmarkSpawnBundle_Escaping2(b *testing.B) {
	const space = 1 << 16
	sp, bs, cs := setupSpawn2(space)
	v := bundle2{Body: Body{X: 1}, Collider: Collider{R: 2}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		e := sp.NewEscaping(v)
		bs.remove(e)
		cs.remove(e)
		sp.ents.free = append(sp.ents.free, e.idx())
	}
}

func BenchmarkSpawnBundle_Hand2(b *testing.B) {
	const space = 1 << 16
	ents := newEntities(space)
	bs, cs := newB[Body](space), newB[Collider](space)
	v := bundle2{Body: Body{X: 1}, Collider: Collider{R: 2}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		e := ents.alloc()
		bs.add(e, v.Body)
		cs.add(e, v.Collider)
		bs.remove(e)
		cs.remove(e)
		ents.free = append(ents.free, e.idx())
	}
}

// --- arity 4 --------------------------------------------------------------

type bundle4 struct {
	Body     Body
	Collider Collider
	Health   Health
	Sprite   Sprite
}

func BenchmarkSpawnBundle_Closures4(b *testing.B) {
	const space = 1 << 16
	ents := newEntities(space)
	bs, cs := newB[Body](space), newB[Collider](space)
	hs, ss := newB[Health](space), newB[Sprite](space)
	sp := &spawner[bundle4]{ents: ents, scratch: new(bundle4), writers: []fieldWriter{
		writerFor(bs, unsafe.Offsetof(bundle4{}.Body)),
		writerFor(cs, unsafe.Offsetof(bundle4{}.Collider)),
		writerFor(hs, unsafe.Offsetof(bundle4{}.Health)),
		writerFor(ss, unsafe.Offsetof(bundle4{}.Sprite)),
	}}
	v := bundle4{Body: Body{X: 1}, Collider: Collider{R: 2}, Health: Health{HP: 3}, Sprite: Sprite{ID: 4}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		e := sp.New(v)
		bs.remove(e)
		cs.remove(e)
		hs.remove(e)
		ss.remove(e)
		ents.free = append(ents.free, e.idx())
	}
}

func BenchmarkSpawnBundle_Hand4(b *testing.B) {
	const space = 1 << 16
	ents := newEntities(space)
	bs, cs := newB[Body](space), newB[Collider](space)
	hs, ss := newB[Health](space), newB[Sprite](space)
	v := bundle4{Body: Body{X: 1}, Collider: Collider{R: 2}, Health: Health{HP: 3}, Sprite: Sprite{ID: 4}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		e := ents.alloc()
		bs.add(e, v.Body)
		cs.add(e, v.Collider)
		hs.add(e, v.Health)
		ss.add(e, v.Sprite)
		bs.remove(e)
		cs.remove(e)
		hs.remove(e)
		ss.remove(e)
		ents.free = append(ents.free, e.idx())
	}
}

// --- where the dispatch cost actually goes --------------------------------

// The dispatcher builds a cancellable context per call whenever the caller is
// not itself bounded (kernel/command.go:103). A subscription's Kernel sets
// bounded: true (kernel/subscription.go:78), so a System dispatching a command
// does NOT take this branch — confirmed by BenchmarkDispatch_ViaUses reporting
// zero allocations against the 4 this costs. It is priced here only so the
// 1113ns dispatch is not mistakenly attributed to context machinery: that cost
// is the scheduler round-trip alone. This branch is what an unbounded caller
// pays, such as one dispatching from outside a handler.
func BenchmarkDispatchPart_ContextSetup(b *testing.B) {
	root := context.Background()
	engineCtx, cancelEngine := context.WithCancel(root)
	defer cancelEngine()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ctx, cancel := context.WithCancel(root)
		stop := context.AfterFunc(engineCtx, cancel)
		stop()
		cancel()
		_ = ctx
	}
}
