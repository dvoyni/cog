package types

import (
	"iter"
	"math/bits"
	"math/rand/v2"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The selectivity sweep (#258): does narrowing a Query's walk by intersecting
// per-Store presence bitsets beat walking the Driver and probing, and can a rule
// chosen once per run pick the faster path? Everything here is a prototype in
// test files; no production file knows a bitset exists.
//
// Every sweep point is the same world shape: 5 000 Body and 5 000 Collider
// Entities with an intersection of 5 000, 2 500, 500, 100 or 50, padded with
// Entities carrying neither to exactly 10 000 live, so len(gens) and the bitset
// length are the same at every point. Velocity and Homing sit on every Body
// Entity, so the width-3 and width-4 Queries tie with Body and Body still
// drives. Disabled is on half the intersection and Solid on all of it.
//
// The four categories are shuffled with a fixed seed before any Entity is
// allocated. Allocation order is index order and every Set appends, so every
// Store's rows stay in index order and both arms walk memory the same way. The
// shuffle, rather than disjointStores' layout, is deliberate: disjointStores
// allocates the whole intersection last, so the last rows of Body are all
// matches and R2, which samples exactly those rows, would read a reject rate of
// zero at every selectivity. The spread layout is the best case for sampling.

const (
	selectivityStore = 5_000
	selectivityPeak  = 10_000
)

// presence is the test-side presence bitset: one []uint64 per Store, one bit
// per Entity index, found by the Store's erased header. A production bitset
// would be a field of Store[T]; here it is looked up once at planning.
type presence struct {
	stores []*storeHeader
	bits   [][]uint64
	words  int
}

func newPresence(peak int) *presence { return &presence{words: (peak + 63) / 64} }

// of is the bitset of one Store, created empty the first time it is asked for.
func (p *presence) of(h *storeHeader) []uint64 {
	for i, s := range p.stores {
		if s == h {
			return p.bits[i]
		}
	}
	b := make([]uint64, p.words)
	p.stores = append(p.stores, h)
	p.bits = append(p.bits, b)
	return b
}

// setBit and clearBit are the whole of what a Store's add and remove would gain.
func setBit(b []uint64, e Entity) {
	i := e.idx()
	b[i>>6] |= 1 << (i & 63)
}

func clearBit(b []uint64, e Entity) {
	i := e.idx()
	b[i>>6] &^= 1 << (i & 63)
}

// place is Store.Set with its presence bit maintained beside it, which is what
// the population helpers do in place of a bare Set.
func place[T any](p *presence, s *Store[T], e Entity, value T) {
	s.Set(e, value)
	setBit(p.of(s.erase()), e)
}

// bitsetPlan is one Query's bitset path, resolved before any timed loop: each
// field's bitset keyed by the field's offset, which travels with the field when
// bind moves the Driver to index 0, and the scratch buffer the snapshot is
// taken into, sized to the peak. A production path would hold the scratch on
// the Query and drop it in Query.release.
type bitsetPlan struct {
	offsets [4]uintptr
	bits    [4][]uint64
	fields  int
	scratch []uint64
}

func planBitset[Q any](q *Query[Q], p *presence) *bitsetPlan {
	if len(q.fields) > 4 {
		panic("the bitset prototype covers widths up to 4")
	}
	plan := &bitsetPlan{fields: len(q.fields), scratch: make([]uint64, p.words)}
	for i := range q.fields {
		plan.offsets[i] = q.fields[i].cursor.offset
		plan.bits[i] = p.of(q.fields[i].get())
	}
	return plan
}

func (pl *bitsetPlan) lookup(offset uintptr) []uint64 {
	for i := range pl.fields {
		if pl.offsets[i] == offset {
			return pl.bits[i]
		}
	}
	panic("no bitset planned for this field")
}

// combine takes the snapshot the walk runs over: the AND of every field matched
// on presence, AND NOT for a Without, in one pass over the words. The snapshot
// is what keeps restructuring the visited Entity safe without a backwards walk:
// a bit set or cleared during the run is not in it.
func (pl *bitsetPlan) combine(fields []queryField) []uint64 {
	var src [4][]uint64
	var flip [4]uint64
	for i := range fields {
		c := fields[i].cursor
		src[i] = pl.lookup(c.offset)
		if c.wanted == absentGeneration {
			flip[i] = ^uint64(0)
		}
	}
	out := pl.scratch
	switch len(fields) {
	case 2:
		a, b := src[0][:len(out)], src[1][:len(out)]
		fa, fb := flip[0], flip[1]
		for w := range out {
			out[w] = (a[w] ^ fa) & (b[w] ^ fb)
		}
	case 3:
		a, b, c := src[0][:len(out)], src[1][:len(out)], src[2][:len(out)]
		fa, fb, fc := flip[0], flip[1], flip[2]
		for w := range out {
			out[w] = (a[w] ^ fa) & (b[w] ^ fb) & (c[w] ^ fc)
		}
	case 4:
		a, b, c, d := src[0][:len(out)], src[1][:len(out)], src[2][:len(out)], src[3][:len(out)]
		fa, fb, fc, fd := flip[0], flip[1], flip[2], flip[3]
		for w := range out {
			out[w] = (a[w] ^ fa) & (b[w] ^ fb) & (c[w] ^ fc) & (d[w] ^ fd)
		}
	default:
		panic("the bitset prototype covers widths 2 to 4")
	}
	return out
}

// presentRow is the dense row of an Entity the snapshot says this field holds.
// A filter fills nothing, and a Without's Store may not reach the index at all,
// so neither is read.
func (c queryCursor) presentRow(index uint32) uintptr {
	if c.size == 0 {
		return 0
	}
	return uintptr(uint32(c.sparse[index]))
}

// bitsetAll is All() with the bitset path, and it has All()'s call shape at
// each width: the two-field walk is written out inside the literal, so the range
// site inlines it with no per-Entity call, and widths 3 and 4 yield through a
// callback from a filler the literal calls, as iterate3 and iterate4 do.
//
// The Entity is rebuilt from the Driver's sparse slot, whose generation half is
// the Entity's own while it holds the Component. A slot found absent is an
// Entity removed during this run, after the snapshot, and is skipped.
func (q *Query[Q]) bitsetAll(pl *bitsetPlan) iter.Seq2[Entity, *Q] {
	return func(yield func(Entity, *Q) bool) {
		if q.shape != 2 {
			q.bitsetIterate(pl, yield)
			return
		}
		q.bind()
		snapshot := pl.combine(q.fields)
		buffer := unsafe.Pointer(&q.rows)
		driver, second := q.fields[0].cursor, q.fields[1].cursor
		for w, word := range snapshot {
			for word != 0 {
				index := uint32(w<<6 | bits.TrailingZeros64(word))
				word &= word - 1
				slot := driver.sparse[index]
				if uint32(slot>>32) == absentGeneration {
					continue
				}
				second.fill(second.presentRow(index), buffer)
				driver.fill(uintptr(uint32(slot)), buffer)
				if !yield(newEntity(index, uint32(slot>>32)), &q.rows) {
					return
				}
			}
		}
	}
}

func (q *Query[Q]) bitsetIterate(pl *bitsetPlan, yield func(Entity, *Q) bool) {
	switch q.shape {
	case 3:
		q.bitsetIterate3(pl, yield)
	case 4:
		q.bitsetIterate4(pl, yield)
	default:
		panic("the bitset prototype covers widths 2 to 4")
	}
}

func (q *Query[Q]) bitsetIterate3(pl *bitsetPlan, yield func(Entity, *Q) bool) {
	q.bind()
	snapshot := pl.combine(q.fields)
	buffer := unsafe.Pointer(&q.rows)
	driver, second, third := q.fields[0].cursor, q.fields[1].cursor, q.fields[2].cursor
	for w, word := range snapshot {
		for word != 0 {
			index := uint32(w<<6 | bits.TrailingZeros64(word))
			word &= word - 1
			slot := driver.sparse[index]
			if uint32(slot>>32) == absentGeneration {
				continue
			}
			second.fill(second.presentRow(index), buffer)
			third.fill(third.presentRow(index), buffer)
			driver.fill(uintptr(uint32(slot)), buffer)
			if !yield(newEntity(index, uint32(slot>>32)), &q.rows) {
				return
			}
		}
	}
}

func (q *Query[Q]) bitsetIterate4(pl *bitsetPlan, yield func(Entity, *Q) bool) {
	q.bind()
	snapshot := pl.combine(q.fields)
	buffer := unsafe.Pointer(&q.rows)
	driver, second := q.fields[0].cursor, q.fields[1].cursor
	third, fourth := q.fields[2].cursor, q.fields[3].cursor
	for w, word := range snapshot {
		for word != 0 {
			index := uint32(w<<6 | bits.TrailingZeros64(word))
			word &= word - 1
			slot := driver.sparse[index]
			if uint32(slot>>32) == absentGeneration {
				continue
			}
			second.fill(second.presentRow(index), buffer)
			third.fill(third.presentRow(index), buffer)
			fourth.fill(fourth.presentRow(index), buffer)
			driver.fill(uintptr(uint32(slot)), buffer)
			if !yield(newEntity(index, uint32(slot>>32)), &q.rows) {
				return
			}
		}
	}
}

// The two Driver rules, each called once per run. true means take the bitset.
//
// R1 predicts the matches from Store lengths under independence, peak × Π(len/
// peak) with a Without contributing (1 − len/peak), and takes the bitset when
// the prediction is below r1Fraction of the Driver's length. peak is len(gens),
// which a production rule would read under the read{*Entities} every System
// holds. Lengths cannot see correlation: at width 2 every sweep point has two
// 5 000-long Stores, so R1 predicts 2 500 at every selectivity.
//
// R2 samples the last r2Sample rows of the Driver, the rows the backwards walk
// visits first, probing every other field without yielding, and takes the
// bitset when the reject rate exceeds r2Reject. It binds to find the Driver and
// the path it dispatches to binds again; that second bind is charged to the rule.
//
// Both rules share one threshold: take the bitset when the match fraction,
// predicted or sampled, is below 0.75. It was set from the pure arms' crossover,
// which is near 50% at width 2 and above it at widths 3 and 4, and from the
// width-3 Without point, where the bitset wins even at 100% overlap because half
// the intersection is excluded. Sixteen rows put a 50% point on the probing side
// of the threshold with a chance of about 4%, where eight rows would be 14%.
const (
	r1Fraction = 0.75
	r2Sample   = 16
	r2Reject   = 0.25
)

func ruleR1[Q any](q *Query[Q], en *Entities) bool {
	peak := float64(len(en.gens))
	expected, driver := peak, -1.0
	for i := range q.fields {
		n := float64(len(q.fields[i].get().owners))
		if q.fields[i].cursor.wanted == absentGeneration {
			expected *= 1 - n/peak
			continue
		}
		expected *= n / peak
		if driver < 0 || n < driver {
			driver = n
		}
	}
	return expected < r1Fraction*driver
}

func ruleR2[Q any](q *Query[Q]) bool {
	q.bind()
	walk := q.walk
	low := max(len(walk)-r2Sample, 0)
	rejected := 0
	for row := len(walk) - 1; row >= low; row-- {
		e := walk[row]
		for i := 1; i < len(q.fields); i++ {
			if _, ok := q.fields[i].cursor.row(e); !ok {
				rejected++
				break
			}
		}
	}
	sampled := len(walk) - low
	return float64(rejected) > r2Reject*float64(sampled)
}

// The sweep's Queries. Body is declared first everywhere and ties with every
// other presence field, so bind keeps it as the Driver; each probing arm
// asserts that.
type (
	selectivityW2 struct {
		Body     *body
		Collider collider
	}
	selectivityW3 struct {
		Body     *body
		Collider collider
		Velocity velocity
	}
	selectivityW4 struct {
		Body     *body
		Collider collider
		Velocity velocity
		Homing   homing
	}
	selectivityW3Without struct {
		Body     *body
		Collider collider
		_        Without[disabled]
	}
)

type (
	selectivityW2System        kernel.Subscription[app.UpdateEvent]
	selectivityW3System        kernel.Subscription[app.UpdateEvent]
	selectivityW4System        kernel.Subscription[app.UpdateEvent]
	selectivityW3WithoutSystem kernel.Subscription[app.UpdateEvent]
	selectivityRemedySystem    kernel.Subscription[app.UpdateEvent]
)

// selectivityWorld is one sweep point: every arm's Query bound by the kernel in
// one world, the presence bitsets, each Query's bitset plan, and the Entities
// each arm must yield.
type selectivityWorld struct {
	entities  *Entities
	bodies    *Store[body]
	w2        *Query[selectivityW2]
	w3        *Query[selectivityW3]
	w4        *Query[selectivityW4]
	w3Without *Query[selectivityW3Without]
	remedy    *Query[withRemedyQuery]
	plan2     *bitsetPlan
	plan3     *bitsetPlan
	plan4     *bitsetPlan
	plan3w    *bitsetPlan
	// both is the intersection, and enabled the part of it without Disabled.
	both    map[Entity]bool
	enabled map[Entity]bool
}

func newSelectivityWorld(tb testing.TB, overlap int) *selectivityWorld {
	tb.Helper()
	w := &selectivityWorld{both: map[Entity]bool{}, enabled: map[Entity]bool{}}
	entities, components, engine := newWorld(tb, selectivityPeak, func(registrar *kernel.Registrar) {
		registrar.Subscribe[selectivityW2System](ToHandler[app.UpdateEvent](registrar, func(q *Query[selectivityW2]) { w.w2 = q }))
		registrar.Subscribe[selectivityW3System](ToHandler[app.UpdateEvent](registrar, func(q *Query[selectivityW3]) { w.w3 = q }))
		registrar.Subscribe[selectivityW4System](ToHandler[app.UpdateEvent](registrar, func(q *Query[selectivityW4]) { w.w4 = q }))
		registrar.Subscribe[selectivityW3WithoutSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[selectivityW3Without]) { w.w3Without = q }))
		registrar.Subscribe[selectivityRemedySystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[withRemedyQuery]) { w.remedy = q }))
	})
	w.entities, w.bodies = entities, components.bodies

	const (
		filler = iota
		bodyOnly
		colliderOnly
		intersection
	)
	only := selectivityStore - overlap
	kinds := make([]uint8, 0, selectivityPeak)
	for range only {
		kinds = append(kinds, bodyOnly, colliderOnly)
	}
	for range overlap {
		kinds = append(kinds, intersection, filler)
	}
	shuffle := rand.New(rand.NewPCG(258, 10_000))
	shuffle.Shuffle(len(kinds), func(i, j int) { kinds[i], kinds[j] = kinds[j], kinds[i] })

	p := newPresence(selectivityPeak)
	disable := false
	for _, kind := range kinds {
		e := entities.alloc()
		if kind == bodyOnly || kind == intersection {
			place(p, components.bodies, e, body{X: 1})
			place(p, components.velocities, e, velocity{X: 1})
			place(p, components.homings, e, homing{Target: e})
		}
		if kind == colliderOnly || kind == intersection {
			place(p, components.colliders, e, collider{Radius: 1})
		}
		if kind == intersection {
			place(p, components.solids, e, solid{})
			w.both[e] = true
			if disable {
				place(p, components.disableds, e, disabled{})
			} else {
				w.enabled[e] = true
			}
			disable = !disable
		}
	}
	// Every Store's bitset exists even when nothing set a bit in it.
	for _, h := range []*storeHeader{components.bodies.erase(), components.colliders.erase(), components.velocities.erase(),
		components.homings.erase(), components.solids.erase(), components.disableds.erase()} {
		p.of(h)
	}
	if n := len(entities.gens); n != selectivityPeak {
		tb.Fatalf("the sweep point has %d live Entities, want %d", n, selectivityPeak)
	}

	frame(tb, engine, 1)
	if w.w2 == nil || w.w3 == nil || w.w4 == nil || w.w3Without == nil || w.remedy == nil {
		tb.Fatal("a sweep System never ran, so its Query was not captured")
	}
	w.plan2 = planBitset(w.w2, p)
	w.plan3 = planBitset(w.w3, p)
	w.plan4 = planBitset(w.w4, p)
	w.plan3w = planBitset(w.w3Without, p)
	return w
}

// selectivityArm is one way through a sweep point.
type selectivityArm uint8

const (
	armProbing selectivityArm = iota
	armBitset
	armR1
	armR2
	armRemedy
)

func (a selectivityArm) String() string {
	return [...]string{"Probing", "Bitset", "R1", "R2", "Remedy"}[a]
}

// selectivityShape is one Query of the sweep.
type selectivityShape uint8

const (
	shapeW2 selectivityShape = iota
	shapeW3
	shapeW4
	shapeW3Without
)

func (s selectivityShape) String() string {
	return [...]string{"W2", "W3", "W4", "W3Without"}[s]
}

// walk returns the arm as one run: a range over the real All(), over
// bitsetAll, or — for a rule arm — a choice made once and then one of the two.
// Every loop body is the one benchmarkDriver uses. Each shape has its own code
// because a range site's inlining depends on the concrete Query type, which is
// exactly what the arms are compared on.
func (w *selectivityWorld) walk(shape selectivityShape, arm selectivityArm) func() int {
	switch shape {
	case shapeW2:
		if arm == armRemedy {
			q := w.remedy
			return func() int {
				n := 0
				for _, it := range q.All() {
					it.Body.X += it.Collider.Radius
					n++
				}
				return n
			}
		}
		q, pl, en := w.w2, w.plan2, w.entities
		return func() int {
			n := 0
			bitset := arm == armBitset || arm == armR1 && ruleR1(q, en) || arm == armR2 && ruleR2(q)
			if bitset {
				for _, it := range q.bitsetAll(pl) {
					it.Body.X += it.Collider.Radius
					n++
				}
				return n
			}
			for _, it := range q.All() {
				it.Body.X += it.Collider.Radius
				n++
			}
			return n
		}
	case shapeW3:
		q, pl, en := w.w3, w.plan3, w.entities
		return func() int {
			n := 0
			bitset := arm == armBitset || arm == armR1 && ruleR1(q, en) || arm == armR2 && ruleR2(q)
			if bitset {
				for _, it := range q.bitsetAll(pl) {
					it.Body.X += it.Collider.Radius
					n++
				}
				return n
			}
			for _, it := range q.All() {
				it.Body.X += it.Collider.Radius
				n++
			}
			return n
		}
	case shapeW4:
		q, pl, en := w.w4, w.plan4, w.entities
		return func() int {
			n := 0
			bitset := arm == armBitset || arm == armR1 && ruleR1(q, en) || arm == armR2 && ruleR2(q)
			if bitset {
				for _, it := range q.bitsetAll(pl) {
					it.Body.X += it.Collider.Radius
					n++
				}
				return n
			}
			for _, it := range q.All() {
				it.Body.X += it.Collider.Radius
				n++
			}
			return n
		}
	default:
		q, pl, en := w.w3Without, w.plan3w, w.entities
		return func() int {
			n := 0
			bitset := arm == armBitset || arm == armR1 && ruleR1(q, en) || arm == armR2 && ruleR2(q)
			if bitset {
				for _, it := range q.bitsetAll(pl) {
					it.Body.X += it.Collider.Radius
					n++
				}
				return n
			}
			for _, it := range q.All() {
				it.Body.X += it.Collider.Radius
				n++
			}
			return n
		}
	}
}

// want is the set an arm must yield: the intersection, less Disabled for the
// Without shape.
func (w *selectivityWorld) want(shape selectivityShape) map[Entity]bool {
	if shape == shapeW3Without {
		return w.enabled
	}
	return w.both
}

// pinProbingDriver asserts that a probing arm walked Body, all of it: without
// that, a probing arm could silently be the remedy.
func (w *selectivityWorld) pinProbingDriver(tb testing.TB, shape selectivityShape) {
	tb.Helper()
	var walked int
	var offset uintptr
	switch shape {
	case shapeW2:
		walked, offset = len(w.w2.walk), w.w2.fields[0].cursor.offset
	case shapeW3:
		walked, offset = len(w.w3.walk), w.w3.fields[0].cursor.offset
	case shapeW4:
		walked, offset = len(w.w4.walk), w.w4.fields[0].cursor.offset
	default:
		walked, offset = len(w.w3Without.walk), w.w3Without.fields[0].cursor.offset
	}
	if walked != selectivityStore || offset != 0 {
		tb.Fatalf("the %s probing walk is %d long off the field at offset %d, want Body's %d off offset 0",
			shape, walked, offset, selectivityStore)
	}
}

// benchmarkSelectivity times one arm at one sweep point, one run per op, and
// asserts every run yielded exactly the intersection's size.
func benchmarkSelectivity(b *testing.B, pct int, shape selectivityShape, arm selectivityArm) {
	overlap := selectivityStore * pct / 100
	w := newSelectivityWorld(b, overlap)
	walk := w.walk(shape, arm)
	want := len(w.want(shape))

	visited := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		visited += walk()
	}
	b.StopTimer()
	if visited != b.N*want {
		b.Fatalf("the %s %s arm yielded %d Entities over %d runs, want %d", shape, arm, visited, b.N, b.N*want)
	}
	if arm == armProbing {
		w.pinProbingDriver(b, shape)
	}
}

var selectivityPoints = []int{100, 50, 10, 2, 1}

// TestEverySelectivityArmYieldsExactlyTheIntersection is what makes the sweep's
// arms comparable: at every point and every shape, every arm yields the
// intersection less the Without's exclusions, each Entity once. The loop body
// adds Collider.Radius, which is 1, to Body.X, so the set an arm yielded is read
// back as the Bodies that moved by exactly 1 and the timed loop needs no hook.
func TestEverySelectivityArmYieldsExactlyTheIntersection(t *testing.T) {
	for _, pct := range selectivityPoints {
		w := newSelectivityWorld(t, selectivityStore*pct/100)
		for _, shape := range []selectivityShape{shapeW2, shapeW3, shapeW4, shapeW3Without} {
			arms := []selectivityArm{armProbing, armBitset, armR1, armR2}
			if shape == shapeW2 {
				arms = append(arms, armRemedy)
			}
			want := w.want(shape)
			for _, arm := range arms {
				before := make(map[Entity]float32, len(w.bodies.owners))
				for row, e := range w.bodies.owners {
					before[e] = w.bodies.dense[row].X
				}
				if n := w.walk(shape, arm)(); n != len(want) {
					t.Errorf("%d%% %s %s: yielded %d, want %d", pct, shape, arm, n, len(want))
				}
				for row, e := range w.bodies.owners {
					moved := w.bodies.dense[row].X - before[e]
					switch {
					case want[e] && moved != 1:
						t.Errorf("%d%% %s %s: %v in the intersection moved by %v, want 1", pct, shape, arm, e, moved)
					case !want[e] && moved != 0:
						t.Errorf("%d%% %s %s: %v outside the intersection moved by %v", pct, shape, arm, e, moved)
					}
				}
			}
			w.walk(shape, armProbing)()
			w.pinProbingDriver(t, shape)
		}
	}
}

// TestTheSelectivityRulesChoose records what each rule picks at every point and
// shape, so the record does not rest on a benchmark's timing alone. R1 cannot
// tell the width-2 points apart: every one has two 5 000-long Stores.
func TestTheSelectivityRulesChoose(t *testing.T) {
	for _, pct := range selectivityPoints {
		w := newSelectivityWorld(t, selectivityStore*pct/100)
		t.Logf("%3d%%  R1: W2 %-5v W3 %-5v W4 %-5v W3Without %-5v  R2: W2 %-5v W3 %-5v W4 %-5v W3Without %-5v", pct,
			ruleR1(w.w2, w.entities), ruleR1(w.w3, w.entities), ruleR1(w.w4, w.entities), ruleR1(w.w3Without, w.entities),
			ruleR2(w.w2), ruleR2(w.w3), ruleR2(w.w4), ruleR2(w.w3Without))
	}
}

// BenchmarkPresenceBitWrite is one set plus one clear: what a Store's add and
// remove would each gain. The indices walk the whole peak so the words are not
// one register.
func BenchmarkPresenceBitWrite(b *testing.B) {
	words := make([]uint64, 8192/64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		setBit(words, newEntity(uint32(i)&8191, 0))
		clearBit(words, newEntity(uint32(i+4096)&8191, 0))
	}
	b.StopTimer()
	sink := uint64(0)
	for _, word := range words {
		sink |= word
	}
	if sink == ^uint64(0) {
		b.Log(sink)
	}
}

type bitsetRestructuringSystem kernel.Subscription[app.UpdateEvent]

// populatePresent is populate with the presence bits maintained beside it.
func populatePresent(p *presence, entities *Entities, components *componentsPlugin, n int) {
	for range n {
		e := entities.alloc()
		place(p, components.bodies, e, body{})
		place(p, components.velocities, e, velocity{X: 1, Y: 2})
	}
}

// TestTheBitsetWalkMayRestructureTheEntityItIsVisiting mirrors
// TestASystemMayRestructureTheEntityItIsVisiting for the bitset arm, which walks
// by index rather than backwards and keeps the guarantee by walking a snapshot.
// The System keeps the live bitsets up to date as it goes, so a walk reading
// them rather than its snapshot would reach the Entities it spawned.
func TestTheBitsetWalkMayRestructureTheEntityItIsVisiting(t *testing.T) {
	const population = 200
	t.Run("despawning the current Entity", func(t *testing.T) {
		visited := 0
		p := newPresence(4 * population)
		var plan *bitsetPlan
		var components *componentsPlugin
		entities, components, engine := newWorld(t, 4*population, func(registrar *kernel.Registrar) {
			registrar.Subscribe[bitsetRestructuringSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[moveQuery], we *WriteableEntities) {
				if plan == nil {
					plan = planBitset(q, p)
				}
				for e := range q.bitsetAll(plan) {
					visited++
					we.Despawn(e)
					clearBit(p.of(components.bodies.erase()), e)
					clearBit(p.of(components.velocities.erase()), e)
				}
			}))
		})
		populatePresent(p, entities, components, population)

		frame(t, engine, 1)

		if visited != population {
			t.Fatalf("a System despawning as it went visited %d of %d Entities", visited, population)
		}
		if components.bodies.Len() != 0 {
			t.Fatalf("%d bodies survived a System that despawned every Entity it visited", components.bodies.Len())
		}
	})

	t.Run("spawning one Entity per visited Entity", func(t *testing.T) {
		visited := 0
		p := newPresence(4 * population)
		var plan *bitsetPlan
		var components *componentsPlugin
		entities, components, engine := newWorld(t, 4*population, func(registrar *kernel.Registrar) {
			registrar.Subscribe[bitsetRestructuringSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[moveQuery], sp *Spawn[spawnSet]) {
				if plan == nil {
					plan = planBitset(q, p)
				}
				for range q.bitsetAll(plan) {
					visited++
					e := sp.New(spawnSet{Body: body{X: 1}, Velocity: velocity{X: 1}})
					setBit(p.of(components.bodies.erase()), e)
					setBit(p.of(components.velocities.erase()), e)
				}
			}))
		})
		populatePresent(p, entities, components, population)

		frame(t, engine, 1)

		if visited != population {
			t.Fatalf("a System spawning as it went visited %d Entities, want the %d it started with: "+
				"the walk reached Entities appended during the loop", visited, population)
		}
		if components.bodies.Len() != 2*population {
			t.Fatalf("the population is %d after one such tick, want %d", components.bodies.Len(), 2*population)
		}
	})
}

// The sweep's benchmarks, one per arm per point, named
// BenchmarkSelectivity<pct>W<width>[Without]<Arm> so each runs alone under
// -test.bench=<name>$. The remedy arm is width 2 at 2% and 1% only.

func BenchmarkSelectivity100W2Probing(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW2, armProbing)
}

func BenchmarkSelectivity100W2Bitset(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW2, armBitset)
}

func BenchmarkSelectivity100W2R1(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW2, armR1)
}

func BenchmarkSelectivity100W2R2(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW2, armR2)
}

func BenchmarkSelectivity100W3Probing(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW3, armProbing)
}

func BenchmarkSelectivity100W3Bitset(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW3, armBitset)
}

func BenchmarkSelectivity100W3R1(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW3, armR1)
}

func BenchmarkSelectivity100W3R2(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW3, armR2)
}

func BenchmarkSelectivity100W4Probing(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW4, armProbing)
}

func BenchmarkSelectivity100W4Bitset(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW4, armBitset)
}

func BenchmarkSelectivity100W4R1(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW4, armR1)
}

func BenchmarkSelectivity100W4R2(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW4, armR2)
}

func BenchmarkSelectivity100W3WithoutProbing(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW3Without, armProbing)
}

func BenchmarkSelectivity100W3WithoutBitset(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW3Without, armBitset)
}

func BenchmarkSelectivity100W3WithoutR1(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW3Without, armR1)
}

func BenchmarkSelectivity100W3WithoutR2(b *testing.B) {
	benchmarkSelectivity(b, 100, shapeW3Without, armR2)
}

func BenchmarkSelectivity50W2Probing(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW2, armProbing)
}

func BenchmarkSelectivity50W2Bitset(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW2, armBitset)
}

func BenchmarkSelectivity50W2R1(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW2, armR1)
}

func BenchmarkSelectivity50W2R2(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW2, armR2)
}

func BenchmarkSelectivity50W3Probing(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW3, armProbing)
}

func BenchmarkSelectivity50W3Bitset(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW3, armBitset)
}

func BenchmarkSelectivity50W3R1(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW3, armR1)
}

func BenchmarkSelectivity50W3R2(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW3, armR2)
}

func BenchmarkSelectivity50W4Probing(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW4, armProbing)
}

func BenchmarkSelectivity50W4Bitset(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW4, armBitset)
}

func BenchmarkSelectivity50W4R1(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW4, armR1)
}

func BenchmarkSelectivity50W4R2(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW4, armR2)
}

func BenchmarkSelectivity50W3WithoutProbing(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW3Without, armProbing)
}

func BenchmarkSelectivity50W3WithoutBitset(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW3Without, armBitset)
}

func BenchmarkSelectivity50W3WithoutR1(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW3Without, armR1)
}

func BenchmarkSelectivity50W3WithoutR2(b *testing.B) {
	benchmarkSelectivity(b, 50, shapeW3Without, armR2)
}

func BenchmarkSelectivity10W2Probing(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW2, armProbing)
}

func BenchmarkSelectivity10W2Bitset(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW2, armBitset)
}

func BenchmarkSelectivity10W2R1(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW2, armR1)
}

func BenchmarkSelectivity10W2R2(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW2, armR2)
}

func BenchmarkSelectivity10W3Probing(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW3, armProbing)
}

func BenchmarkSelectivity10W3Bitset(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW3, armBitset)
}

func BenchmarkSelectivity10W3R1(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW3, armR1)
}

func BenchmarkSelectivity10W3R2(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW3, armR2)
}

func BenchmarkSelectivity10W4Probing(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW4, armProbing)
}

func BenchmarkSelectivity10W4Bitset(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW4, armBitset)
}

func BenchmarkSelectivity10W4R1(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW4, armR1)
}

func BenchmarkSelectivity10W4R2(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW4, armR2)
}

func BenchmarkSelectivity10W3WithoutProbing(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW3Without, armProbing)
}

func BenchmarkSelectivity10W3WithoutBitset(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW3Without, armBitset)
}

func BenchmarkSelectivity10W3WithoutR1(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW3Without, armR1)
}

func BenchmarkSelectivity10W3WithoutR2(b *testing.B) {
	benchmarkSelectivity(b, 10, shapeW3Without, armR2)
}

func BenchmarkSelectivity2W2Probing(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW2, armProbing)
}

func BenchmarkSelectivity2W2Bitset(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW2, armBitset)
}

func BenchmarkSelectivity2W2R1(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW2, armR1)
}

func BenchmarkSelectivity2W2R2(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW2, armR2)
}

func BenchmarkSelectivity2W2Remedy(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW2, armRemedy)
}

func BenchmarkSelectivity2W3Probing(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW3, armProbing)
}

func BenchmarkSelectivity2W3Bitset(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW3, armBitset)
}

func BenchmarkSelectivity2W3R1(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW3, armR1)
}

func BenchmarkSelectivity2W3R2(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW3, armR2)
}

func BenchmarkSelectivity2W4Probing(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW4, armProbing)
}

func BenchmarkSelectivity2W4Bitset(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW4, armBitset)
}

func BenchmarkSelectivity2W4R1(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW4, armR1)
}

func BenchmarkSelectivity2W4R2(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW4, armR2)
}

func BenchmarkSelectivity2W3WithoutProbing(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW3Without, armProbing)
}

func BenchmarkSelectivity2W3WithoutBitset(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW3Without, armBitset)
}

func BenchmarkSelectivity2W3WithoutR1(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW3Without, armR1)
}

func BenchmarkSelectivity2W3WithoutR2(b *testing.B) {
	benchmarkSelectivity(b, 2, shapeW3Without, armR2)
}

func BenchmarkSelectivity1W2Probing(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW2, armProbing)
}

func BenchmarkSelectivity1W2Bitset(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW2, armBitset)
}

func BenchmarkSelectivity1W2R1(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW2, armR1)
}

func BenchmarkSelectivity1W2R2(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW2, armR2)
}

func BenchmarkSelectivity1W2Remedy(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW2, armRemedy)
}

func BenchmarkSelectivity1W3Probing(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW3, armProbing)
}

func BenchmarkSelectivity1W3Bitset(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW3, armBitset)
}

func BenchmarkSelectivity1W3R1(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW3, armR1)
}

func BenchmarkSelectivity1W3R2(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW3, armR2)
}

func BenchmarkSelectivity1W4Probing(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW4, armProbing)
}

func BenchmarkSelectivity1W4Bitset(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW4, armBitset)
}

func BenchmarkSelectivity1W4R1(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW4, armR1)
}

func BenchmarkSelectivity1W4R2(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW4, armR2)
}

func BenchmarkSelectivity1W3WithoutProbing(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW3Without, armProbing)
}

func BenchmarkSelectivity1W3WithoutBitset(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW3Without, armBitset)
}

func BenchmarkSelectivity1W3WithoutR1(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW3Without, armR1)
}

func BenchmarkSelectivity1W3WithoutR2(b *testing.B) {
	benchmarkSelectivity(b, 1, shapeW3Without, armR2)
}
