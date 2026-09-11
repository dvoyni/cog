package sbench

import (
	"math/rand"
	"testing"
)

// Structural change, for cog#240. Four questions, all measured on storeB, the
// shape cog#239 chose:
//
//  1. Does removing the current entity mid-iteration have to be forbidden, or
//     does walking the dense array backwards make it safe? (TestSwapRemove*)
//  2. What does backwards cost? (BenchmarkIter_Forward / _Reverse)
//  3. Can a deferred queue be allocation-free in the steady state, and does
//     type erasure — bevy's `Commands` shape — break that? (BenchmarkDefer*)
//  4. What does an eager despawn cost when nothing records which Stores hold
//     an entity, so every Store must be asked? (BenchmarkDespawn_ScanAll)
type remover interface {
	remove(e Entity)
}

// swap-remove, the removal cog#239 specified.
func (s *storeB[T]) remove(e Entity) {
	v := s.sparse[e.idx()]
	if uint32(v>>32) != e.gen() {
		return
	}
	d := uint32(v)
	last := uint32(len(s.dense) - 1)
	if d != last {
		moved := s.owners[last]
		s.dense[d] = s.dense[last]
		s.owners[d] = moved
		s.sparse[moved.idx()] = uint64(moved.gen())<<32 | uint64(d)
	}
	s.dense = s.dense[:last]
	s.owners = s.owners[:last]
	s.sparse[e.idx()] = absentGen64
}

func (s *storeB[T]) has(e Entity) bool {
	return uint32(s.sparse[e.idx()]>>32) == e.gen()
}

func fill(n int) *storeB[Body] {
	s := newB[Body](uint32(n))
	for i := range n {
		s.add(mkEntity(uint32(i), 1), Body{X: float64(i)})
	}
	return s
}

// --- 1. correctness: does swap-remove during iteration skip entities? -------

// Forward, written the natural way: remove every entity whose X is even,
// walking dense 0..len. Swap-remove drops the tail element into the hole at i,
// and i++ steps straight over it.
func TestSwapRemoveForwardSkips(t *testing.T) {
	s := fill(1000)
	seen := map[float64]bool{}
	for i := 0; i < len(s.dense); i++ {
		seen[s.dense[i].X] = true
		if int(s.dense[i].X)%2 == 0 {
			s.remove(s.owners[i])
		}
	}
	if len(seen) == 1000 {
		t.Fatalf("forward iteration visited all 1000 — expected it to skip")
	}
	t.Logf("forward + swap-remove visited %d of 1000 entities", len(seen))
}

// Forward is salvageable, but only by compensating: step i back so the next
// iteration re-reads the slot the tail element landed in. It is correct and it
// is exactly the line a user forgets — and, being a rewrite of the loop
// variable, it is a thing an iteration API can express only by owning the loop.
func TestSwapRemoveForwardWithDecrementIsCorrect(t *testing.T) {
	s := fill(1000)
	seen := map[float64]bool{}
	for i := 0; i < len(s.dense); i++ {
		seen[s.dense[i].X] = true
		if int(s.dense[i].X)%2 == 0 {
			s.remove(s.owners[i])
			i--
		}
	}
	if len(seen) != 1000 {
		t.Fatalf("forward + decrement visited %d of 1000, want all", len(seen))
	}
}

// Reverse: the same removal, walking dense len-1..0. Swap-remove moves the
// tail element into the hole, and the tail is already behind us.
func TestSwapRemoveReverseVisitsAll(t *testing.T) {
	s := fill(1000)
	seen := map[float64]bool{}
	for i := len(s.dense) - 1; i >= 0; i-- {
		seen[s.dense[i].X] = true
		if int(s.dense[i].X)%2 == 0 {
			s.remove(s.owners[i])
		}
	}
	if len(seen) != 1000 {
		t.Fatalf("reverse iteration visited %d of 1000, want all", len(seen))
	}
	if len(s.dense) != 500 {
		t.Fatalf("after removal len(dense) = %d, want 500", len(s.dense))
	}
}

// Reverse iteration also hides an append: add() puts the new entity at the end,
// which a backwards walk started before the append never reaches.
func TestReverseHidesAppend(t *testing.T) {
	s := newB[Body](1000)
	for i := range 100 {
		s.add(mkEntity(uint32(i), 1), Body{X: float64(i)})
	}
	visited := 0
	for i := len(s.dense) - 1; i >= 0; i-- {
		visited++
		if i == 50 {
			s.add(mkEntity(uint32(500+i), 1), Body{X: -1})
		}
	}
	if visited != 100 {
		t.Fatalf("visited %d, want exactly the 100 present at loop entry", visited)
	}
}

// Forward does the opposite, and this is the hazard that has no compensating
// line: an entity appended during the loop IS reached by it, so a System that
// spawns per visited entity feeds its own iteration. Capped here so a failure
// reports instead of hanging.
func TestForwardSeesAppend(t *testing.T) {
	s := newB[Body](100000)
	for i := range 100 {
		s.add(mkEntity(uint32(i), 1), Body{X: float64(i)})
	}
	visited := 0
	for i := 0; i < len(s.dense); i++ {
		visited++
		if visited > 10000 {
			break // runaway confirmed
		}
		s.add(mkEntity(uint32(1000+visited), 1), Body{X: -1})
	}
	if visited <= 10000 {
		t.Fatalf("visited %d, expected forward iteration to run away", visited)
	}
	t.Logf("forward iteration visited %d of an initial 100, spawning one entity per visit — it never terminates", visited)
}

// But reverse only makes removing the CURRENT entity safe. Removing some other
// entity can still relocate one we have not reached.
func TestReverseArbitraryRemovalStillSkips(t *testing.T) {
	s := fill(1000)
	seen := map[float64]bool{}
	for i := len(s.dense) - 1; i >= 0; i-- {
		seen[s.dense[i].X] = true
		if i == 900 {
			s.remove(s.owners[10]) // not the current entity
		}
	}
	if len(seen) == 1000 {
		t.Fatalf("expected removing a non-current entity to skip one")
	}
	t.Logf("reverse + arbitrary removal visited %d of 1000", len(seen))
}

// --- 2. what backwards costs ----------------------------------------------

const structN = 5000

func BenchmarkIter_Forward(b *testing.B) {
	s := fill(structN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := 0; i < len(s.dense); i++ {
			t += s.dense[i].X
		}
		sink = t
	}
}

func BenchmarkIter_Reverse(b *testing.B) {
	s := fill(structN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := len(s.dense) - 1; i >= 0; i-- {
			t += s.dense[i].X
		}
		sink = t
	}
}

// With a probe into a second Store, which is the realistic shape: the driver
// walks its own dense array, every other Component is probed at random.
func BenchmarkIterProbe_Forward(b *testing.B) {
	s, c := fill(structN), newB[Collider](structN)
	for _, e := range s.owners {
		c.add(e, Collider{R: 1})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := 0; i < len(s.dense); i++ {
			if v, ok := c.get(s.owners[i]); ok {
				t += s.dense[i].X + v.R
			}
		}
		sink = t
	}
}

func BenchmarkIterProbe_Reverse(b *testing.B) {
	s, c := fill(structN), newB[Collider](structN)
	for _, e := range s.owners {
		c.add(e, Collider{R: 1})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := len(s.dense) - 1; i >= 0; i-- {
			if v, ok := c.get(s.owners[i]); ok {
				t += s.dense[i].X + v.R
			}
		}
		sink = t
	}
}

// --- 3. deferral, and whether it can be allocation-free -------------------

// The nox shape: a projectile whose lifetime expired despawns itself. 5000
// entities, 1 in 100 dies this tick — ~50 structural events per tick, which is
// what cog#235 computed for 1000 live projectiles.
const dieEvery = 100

// (a) immediate: reverse-iterate and remove in place, then put the same
// entities straight back, so the Store's population is stationary and every
// variant below does identical work — the only difference is when it happens.
func BenchmarkStructural_Immediate(b *testing.B) {
	s := fill(structN)
	back := make([]Entity, 0, 256)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := len(s.dense) - 1; i >= 0; i-- {
			if int(s.dense[i].X)%dieEvery == 0 {
				e := s.owners[i]
				back = append(back, e)
				s.remove(e)
			}
		}
		n := len(back)
		for _, e := range back {
			s.add(e, Body{X: float64(e.idx())})
		}
		back = back[:0]
		sink = float64(n)
	}
}

// (b) deferred into a typed queue owned by the Store, retained across ticks.
// This is the shape a per-Store pending buffer would have: no type erasure,
// because the queue lives inside Store[T] and already knows T.
type pendingB[T any] struct {
	*storeB[T]
	kill []Entity
	add  []pendingAdd[T]
}

type pendingAdd[T any] struct {
	e Entity
	v T
}

func (p *pendingB[T]) drain() {
	for _, e := range p.kill {
		p.remove(e)
	}
	for _, a := range p.add {
		p.storeB.add(a.e, a.v)
	}
	p.kill = p.kill[:0]
	p.add = p.add[:0]
}

func BenchmarkStructural_DeferredTyped(b *testing.B) {
	p := &pendingB[Body]{storeB: fill(structN)}
	p.drain() // warm the queues to steady state
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := len(p.dense) - 1; i >= 0; i-- {
			if int(p.dense[i].X)%dieEvery == 0 {
				p.kill = append(p.kill, p.owners[i])
			}
		}
		n := len(p.kill)
		// Put them straight back, so the Store's population is stationary and
		// the benchmark measures the queue rather than a shrinking array.
		for _, e := range p.kill {
			p.add = append(p.add, pendingAdd[Body]{e, Body{X: float64(e.idx())}})
		}
		p.drain()
		sink = float64(n)
	}
}

// (c) deferred into a type-erased buffer — bevy's `Commands` shape, where one
// queue carries commands for every Component type and so must box the value.
type erasedCmd struct {
	e   Entity
	val any // nil means despawn
}

func BenchmarkStructural_DeferredErased(b *testing.B) {
	s := fill(structN)
	buf := make([]erasedCmd, 0, 256)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := len(s.dense) - 1; i >= 0; i-- {
			if int(s.dense[i].X)%dieEvery == 0 {
				buf = append(buf, erasedCmd{e: s.owners[i]})
			}
		}
		n := len(buf)
		for i := range buf {
			buf[i].val = Body{X: float64(buf[i].e.idx())}
		}
		for _, c := range buf {
			s.remove(c.e)
		}
		for _, c := range buf {
			s.add(c.e, c.val.(Body))
		}
		buf = buf[:0]
		sink = float64(n)
	}
}

// --- 4. eager despawn with no per-entity record of which Stores hold it ----

// Nothing indexes "which Stores contain entity e" — cog#180 rules a global
// index out, since every structural change would have to write it. So an eager
// despawn asks every Store. This measures that scan at two widths: 8 Component
// types, and nox's 85 class/flag/status Tags.
func benchDespawnScan(b *testing.B, nStores int) {
	stores := make([]*storeB[struct{}], nStores)
	for i := range stores {
		stores[i] = newB[struct{}](structN)
	}
	ids := make([]Entity, structN)
	for i := range ids {
		ids[i] = mkEntity(uint32(i), 1)
	}
	// Each entity is in a handful of Stores, as a real entity would be.
	rng := rand.New(rand.NewSource(7))
	for _, e := range ids {
		for k := 0; k < 5; k++ {
			st := stores[rng.Intn(nStores)]
			if !st.has(e) {
				st.add(e, struct{}{})
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	var hits int
	for i := range b.N {
		e := ids[i%len(ids)]
		for _, st := range stores {
			if st.has(e) {
				hits++
			}
		}
	}
	sink = float64(hits)
}

func BenchmarkDespawn_ScanAll_8(b *testing.B)  { benchDespawnScan(b, 8) }
func BenchmarkDespawn_ScanAll_85(b *testing.B) { benchDespawnScan(b, 85) }

// --- 5. spawn, the hot path nox actually cares about ----------------------

// A projectile: four Components, into pre-grown Stores.
func BenchmarkSpawn_FourComponents(b *testing.B) {
	const space = 1 << 16
	bs, cs := newB[Body](space), newB[Collider](space)
	hs, ss := newB[Health](space), newB[Sprite](space)
	// Grow once so the benchmark measures steady state, not append doubling.
	for i := range space {
		e := mkEntity(uint32(i), 1)
		bs.add(e, Body{})
		cs.add(e, Collider{})
		hs.add(e, Health{})
		ss.add(e, Sprite{})
	}
	for i := range space {
		e := mkEntity(uint32(i), 1)
		bs.remove(e)
		cs.remove(e)
		hs.remove(e)
		ss.remove(e)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		e := mkEntity(uint32(i%space), 1)
		bs.add(e, Body{X: 1})
		cs.add(e, Collider{R: 2})
		hs.add(e, Health{HP: 3})
		ss.add(e, Sprite{ID: 4})
		bs.remove(e)
		cs.remove(e)
		hs.remove(e)
		ss.remove(e)
	}
}
