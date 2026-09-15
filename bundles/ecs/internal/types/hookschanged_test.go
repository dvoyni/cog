package types

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The behaviour tests of hooks.md § Changed is a difference in bytes, on
// Systems in a real kernel.Engine, invoked as commands as in hooks_test.go.

// changesIn counts the Changed records a Store's log holds, per Entity.
func changesIn[T any](log *hookLog[T]) map[Entity]int {
	counts := map[Entity]int{}
	for _, r := range log.records {
		if r.kinds == kindChanged {
			counts[r.e]++
		}
	}
	return counts
}

// kindsIn renders a log's records as their kinds, in order.
func kindsIn[T any](log *hookLog[T]) []string {
	var kinds []string
	for _, r := range log.records {
		var names []string
		for _, kind := range []struct {
			bit  hookKind
			name string
		}{{kindSpawned, "spawned"}, {kindDespawned, "despawned"}, {kindAdded, "added"}, {kindRemoved, "removed"}, {kindChanged, "changed"}} {
			if r.kinds&kind.bit != 0 {
				names = append(names, kind.name)
			}
		}
		kinds = append(kinds, strings.Join(names, "+"))
	}
	return kinds
}

// byEntity orders delivered records by Entity, for a test about which records
// there are rather than their order.
func byEntity(records []heard) []heard {
	sorted := slices.Clone(records)
	slices.SortStableFunc(sorted, func(a, b heard) int { return int(a.e.idx()) - int(b.e.idx()) })
	return sorted
}

// TestAChangeInBytesRecordsOneChangedPerEntityPerWriterRun is hooks.md § Changed
// is a difference in bytes, for each write route: a *T Query field, Ref and a
// replacing UpdateFor. Writing an Entity several times in one run, through one
// route or all three, records one Changed; writing the bytes it already held, or
// writing and restoring them, records nothing.
func TestAChangeInBytesRecordsOneChangedPerEntityPerWriterRun(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAddedChanged]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	ids := make([]Entity, 8)
	for i := range ids {
		ids[i] = w.entities.alloc()
		w.components.colliders.Set(ids[i], collider{Radius: 1})
	}
	byField, byRef, byUpdate, byAll, same, restored, refFirst, untouched := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7]
	expectOnce := func(what string, want []heard) {
		t.Helper()
		counts := changesIn(w.components.colliders.hooks)
		for _, h := range want {
			if counts[h.e] != 1 {
				t.Fatalf("%s: the log holds %d Changed records for %v, want 1: %v", what, counts[h.e], h.e, counts)
			}
		}
		if len(counts) != len(want) {
			t.Fatalf("%s: the log holds Changed records for %v, want only %v", what, counts, want)
		}
		w.read(t)
		expectHeard(t, what, byEntity(got), want)
	}

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		ref, _ := set.Ref(byRef)
		ref.Radius = 2
		ref, _ = set.Ref(byRef)
		ref.Radius = 3
		set.UpdateFor(byUpdate, collider{Radius: 2})
		set.UpdateFor(byUpdate, collider{Radius: 4})
		ref, _ = set.Ref(byAll)
		ref.Radius = 5
		set.UpdateFor(byAll, collider{Radius: 6})
		set.UpdateFor(same, collider{Radius: 1})
		ref, _ = set.Ref(same)
		ref.Radius = 1
		set.UpdateFor(restored, collider{Radius: 9})
		ref, _ = set.Ref(restored)
		ref.Radius = 1
		set.Ref(untouched)
	})
	expectOnce("Ref and UpdateFor", []heard{
		{byRef, "changed", 3},
		{byUpdate, "changed", 4},
		{byAll, "changed", 6},
	})

	w.walked(t, func(q *Query[colliderQuery], set *Set[collider], remove *Remove[collider]) {
		// A row copied through Ref before the Query binds keeps the bytes it held
		// before this write, so the whole-Store copy does not hide it.
		ref, _ := set.Ref(refFirst)
		ref.Radius = 7
		for e, it := range q.All() {
			switch e {
			case byField:
				it.Collider.Radius = 2
				it.Collider.Radius = 3
			case same:
				it.Collider.Radius = 1
			case restored:
				it.Collider.Radius = 8
			}
		}
		for e, it := range q.All() {
			if e == restored {
				it.Collider.Radius = 1
			}
		}
		ref, _ = set.Ref(byAll)
		ref.Radius = 10
		set.UpdateFor(byAll, collider{Radius: 11})
	})
	expectOnce("a *T field beside Ref and UpdateFor", []heard{
		{byField, "changed", 3},
		{byAll, "changed", 11},
		{refFirst, "changed", 7},
	})
}

// TestTheWindowExampleIsDeliveredAsStated is the example in hooks.md § Values,
// and a window, made by four writer runs and read under HookAll: a change folds
// into the addition that opened its window, and the addition carries the value
// at the removal that closed it.
func TestTheWindowExampleIsDeliveredAsStated(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAll]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	e := w.entities.alloc()

	w.write(t, func(set *Set[collider], remove *Remove[collider]) { set.UpdateFor(e, collider{Radius: 10}) })
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		ref, _ := set.Ref(e)
		ref.Radius = 11
	})
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		remove.From(e)
		set.UpdateFor(e, collider{Radius: 12})
	})
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		ref, _ := set.Ref(e)
		ref.Radius = 13
	})

	wantLog := []string{"added+changed", "changed", "removed", "added+changed", "changed"}
	if kinds := kindsIn(w.components.colliders.hooks); !slices.Equal(kinds, wantLog) {
		t.Fatalf("the log holds %v, want %v", kinds, wantLog)
	}
	w.read(t)
	expectHeard(t, "HookAll", got, []heard{
		{e, "added+changed", 11},
		{e, "removed", 11},
		{e, "added+changed", 13},
	})
}

// TestAStandaloneChangedSitsAtItsWindowsFirstChange is hooks.md § Values, and a
// window, for Entities that held T before the reader's copy began: each window
// delivers one Changed, at the position of its first change, carrying the value
// at the next removal or at the reader's run start.
func TestAStandaloneChangedSitsAtItsWindowsFirstChange(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAll]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	first, second := w.entities.alloc(), w.entities.alloc()
	w.components.colliders.Set(first, collider{Radius: 1})
	w.components.colliders.Set(second, collider{Radius: 1})
	change := func(e Entity, value float32) {
		w.write(t, func(set *Set[collider], remove *Remove[collider]) {
			ref, _ := set.Ref(e)
			ref.Radius = value
		})
	}

	change(first, 2)
	change(second, 3)
	change(first, 4)
	w.read(t)
	expectHeard(t, "three changes in two windows", got, []heard{
		{first, "changed", 4},
		{second, "changed", 3},
	})

	change(second, 5)
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		remove.From(second)
		ref, _ := set.Ref(first)
		ref.Radius = 6
	})
	w.read(t)
	expectHeard(t, "a change closed by a removal", got, []heard{
		{second, "changed", 5},
		{second, "removed", 5},
		{first, "changed", 6},
	})
}

// TestASystemNeverSeesItsOwnChanged is hooks.md § A System never sees its own
// Changed, for a System that writes collider and reads its Hooks: its own
// Changed records are not given to it, its own addition is, with IsChanged true,
// and its own writes are in the values later records carry. Another writer's
// change is given to it.
func TestASystemNeverSeesItsOwnChanged(t *testing.T) {
	var got []heard
	var own func(set *Set[collider])
	w := newHookWorld(t, func(h *Hooks[collider, HookAll], set *Set[collider]) {
		got = got[:0]
		listen(h, &got, radius)
		if own != nil {
			own(set)
		}
	})
	held, added, other := w.entities.alloc(), w.entities.alloc(), w.entities.alloc()
	w.components.colliders.Set(held, collider{Radius: 1})
	w.components.colliders.Set(other, collider{Radius: 1})

	own = func(set *Set[collider]) {
		ref, _ := set.Ref(held)
		ref.Radius = 2
		set.UpdateFor(added, collider{Radius: 3})
	}
	w.read(t)
	if counts := changesIn(w.components.colliders.hooks); counts[held] != 1 {
		t.Fatalf("the System's own change to %v is not in the log: %v", held, counts)
	}
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		ref, _ := set.Ref(other)
		ref.Radius = 4
	})
	own = func(set *Set[collider]) {
		ref, _ := set.Ref(added)
		ref.Radius = 5
	}
	w.read(t)
	expectHeard(t, "the run after its own change and addition", got, []heard{
		{added, "added+changed", 3},
		{other, "changed", 4},
	})

	own = nil
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		remove.From(added)
		remove.From(held)
	})
	w.read(t)
	expectHeard(t, "removals after its own writes", got, []heard{
		{added, "removed", 5},
		{held, "removed", 2},
	})
}

// TestAChangeFollowedByARemovalRecordsOnlyTheRemoval is hooks.md's Settled here
// of that name, through Ref and through a *T field: the row is gone by the run
// end, so it compares against nothing.
func TestAChangeFollowedByARemovalRecordsOnlyTheRemoval(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAll]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	byRef, byField := w.entities.alloc(), w.entities.alloc()
	w.components.colliders.Set(byRef, collider{Radius: 1})
	w.components.colliders.Set(byField, collider{Radius: 1})

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		ref, _ := set.Ref(byRef)
		ref.Radius = 2
		remove.From(byRef)
	})
	w.walked(t, func(q *Query[colliderQuery], set *Set[collider], remove *Remove[collider]) {
		for e, it := range q.All() {
			if e == byField {
				it.Collider.Radius = 3
				remove.From(e)
			}
		}
	})
	if kinds := kindsIn(w.components.colliders.hooks); !slices.Equal(kinds, []string{"removed", "removed"}) {
		t.Fatalf("the log holds %v, want two removals and no change", kinds)
	}
	w.read(t)
	expectHeard(t, "HookAll", got, []heard{{byRef, "removed", 2}, {byField, "removed", 3}})
}

type (
	tagWriteCmd kernel.Command[hookRequest, hookResponse]
	tagReadCmd  kernel.Command[hookRequest, hookResponse]
)

type disabledQuery struct{ Disabled *disabled }

// TestATagNeverRecordsChanged is hooks.md § Which acts are recorded: a Component
// with no fields has no bytes to differ, so no write route records a change on
// it, and HookAddedChanged on a Tag delivers additions only.
func TestATagNeverRecordsChanged(t *testing.T) {
	var got []string
	var fresh Entity
	entities, components, engine := newWorld(t, 16, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[tagWriteCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(q *Query[disabledQuery], set *Set[disabled]) {
				for e, it := range q.All() {
					*it.Disabled = disabled{}
					set.UpdateFor(e, disabled{})
					set.Ref(e)
				}
				set.UpdateFor(fresh, disabled{})
			}))
		registrar.HandleCommand[tagReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[disabled, HookAddedChanged]) {
				got = got[:0]
				for e, hook := range h.All() {
					got = append(got, fmt.Sprintf("%v added=%v changed=%v", e, hook.IsAdded(), hook.IsChanged()))
				}
			}))
	})
	held := entities.alloc()
	components.disableds.Set(held, disabled{})
	fresh = entities.alloc()
	for range 2 {
		if _, err := engine.Executioner().ExecuteCommand[tagWriteCmd](hookRequest{}); err != nil {
			t.Fatalf("running the writer: %v", err)
		}
	}
	if kinds := kindsIn(components.disableds.hooks); !slices.Equal(kinds, []string{"added+changed"}) {
		t.Fatalf("writing a Tag recorded %v, want only the one addition", kinds)
	}
	if _, err := engine.Executioner().ExecuteCommand[tagReadCmd](hookRequest{}); err != nil {
		t.Fatalf("running the reader: %v", err)
	}
	want := []string{fmt.Sprintf("%v added=true changed=true", fresh)}
	if !slices.Equal(got, want) {
		t.Fatalf("HookAddedChanged on a Tag delivered %v, want %v", got, want)
	}
}

// padded is a Component whose padding is written out: 7 bytes after Flag and 6
// after Small. Three fields, so Go passes and returns it in registers.
type padded struct {
	Flag  bool
	_     [7]byte
	Value int64
	Small int16
	_     [6]byte
}

// paddedWide has explicit padding after every bool and is too wide to travel in
// registers, so it moves through memory.
type paddedWide struct {
	A bool
	_ [7]byte
	B int64
	C bool
	_ [7]byte
	D int64
	E bool
	_ [7]byte
	F int64
}

// packed is a Component holding a List beside a plain field, with its tail
// padding spelled out, as a Component watched for Changed must.
type packed struct {
	Items List[int32]
	X     float32
	_     [4]byte
}

// changedOwnerPlugin owns the Components these tests need beyond the shared
// ones, and subscribes its own Systems in its own Register, which the kernel runs
// before any plugin depending on it.
type changedOwnerPlugin struct {
	padded *Store[padded]
	wide   *Store[paddedWide]
	packed *Store[packed]
	own    func(*kernel.Registrar)
}

func (p *changedOwnerPlugin) Name() kernel.PluginName { return "changedowner" }

func (p *changedOwnerPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *changedOwnerPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.padded = RegisterComponent[padded](registrar, 16)
	p.wide = RegisterComponent[paddedWide](registrar, 16)
	p.packed = RegisterComponent[packed](registrar, 16)
	if p.own != nil {
		p.own(registrar)
	}
	return nil
}

// newChangedWorld composes the owner, with its own Systems, beside the systems
// plugin, which depends on it.
func newChangedWorld(t *testing.T, subscribe, own func(*kernel.Registrar)) (*Entities, *changedOwnerPlugin, *kernel.Engine) {
	t.Helper()
	owner := &changedOwnerPlugin{own: own}
	entities, _, engine := newWorldWith(t, 16, subscribe, []kernel.PluginName{Name, "components", "changedowner"}, owner)
	return entities, owner, engine
}

type (
	packedWriteCmd kernel.Command[hookRequest, hookResponse]
	packedReadCmd  kernel.Command[hookRequest, hookResponse]
)

type packedQuery struct{ Packed *packed }

// TestAListSetThroughTheStoredListRecordsChanged is hooks.md § A List shows its
// changes in its header: a Set through the stored List, reached by Ref or by a
// *T field, bumps the generation in the row's bytes, so it records Changed even
// when the element written is equal to the one it replaced.
func TestAListSetThroughTheStoredListRecordsChanged(t *testing.T) {
	var got []string
	var write func(q *Query[packedQuery], set *Set[packed])
	entities, owner, engine := newChangedWorld(t, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[packedWriteCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(q *Query[packedQuery], set *Set[packed]) { write(q, set) }))
		registrar.HandleCommand[packedReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[packed, HookAddedChanged]) {
				for e, hook := range h.All() {
					var items []int32
					for _, item := range hook.Value.Items.All() {
						items = append(items, item)
					}
					got = append(got, fmt.Sprintf("%v %v %v", e, hook.IsChanged(), items))
				}
			}))
	}, nil)
	byRef, byField, untouched := entities.alloc(), entities.alloc(), entities.alloc()
	for _, e := range []Entity{byRef, byField, untouched} {
		owner.packed.Set(e, packed{Items: NewList[int32](1, 2)})
	}

	write = func(q *Query[packedQuery], set *Set[packed]) {
		ref, _ := set.Ref(byRef)
		ref.Items.Set(0, 1)
		for e, it := range q.All() {
			if e == byField {
				it.Packed.Items.Set(1, 9)
			}
		}
	}
	if _, err := engine.Executioner().ExecuteCommand[packedWriteCmd](hookRequest{}); err != nil {
		t.Fatalf("running the writer: %v", err)
	}
	counts := changesIn(owner.packed.hooks)
	if counts[byRef] != 1 || counts[byField] != 1 || len(counts) != 2 {
		t.Fatalf("the log holds Changed records %v, want one each for %v and %v", counts, byRef, byField)
	}
	if _, err := engine.Executioner().ExecuteCommand[packedReadCmd](hookRequest{}); err != nil {
		t.Fatalf("running the reader: %v", err)
	}
	slices.Sort(got)
	want := []string{fmt.Sprintf("%v true [1 2]", byRef), fmt.Sprintf("%v true [1 9]", byField)}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the reader was given %v, want %v", got, want)
	}
}

type (
	ownerWriterSystem     kernel.Subscription[app.UpdateEvent]
	dependentReaderSystem kernel.Subscription[app.UpdateEvent]
)

type paddedWriteQuery struct{ Padded *padded }

// TestAReaderRegisteredAfterTheOwnersWriterGetsOneChangedPerEntityPerTick is
// hooks.md's registration order: the owner's writer, a *T Query field,
// registers in the owner's plugin before the dependent plugin's HookAddedChanged
// reader exists, and the reader still gets exactly one Changed per written
// Entity per tick.
func TestAReaderRegisteredAfterTheOwnersWriterGetsOneChangedPerEntityPerTick(t *testing.T) {
	const n, ticks = 16, 5
	var ticked []map[Entity]string
	entities, owner, engine := newChangedWorld(t,
		func(registrar *kernel.Registrar) {
			registrar.Subscribe[dependentReaderSystem](ToHandler[app.UpdateEvent](registrar,
				func(h *Hooks[padded, HookAddedChanged]) {
					heard := map[Entity]string{}
					for e, hook := range h.All() {
						heard[e] += fmt.Sprintf("added=%v changed=%v;", hook.IsAdded(), hook.IsChanged())
					}
					ticked = append(ticked, heard)
				})).After[ownerWriterSystem]()
		},
		func(registrar *kernel.Registrar) {
			registrar.Subscribe[ownerWriterSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[paddedWriteQuery]) {
				for _, it := range q.All() {
					it.Padded.Value++
				}
			}))
		})
	written := make([]Entity, n)
	for i := range written {
		written[i] = entities.alloc()
		owner.padded.Set(written[i], padded{Value: int64(i)})
	}
	for range ticks {
		frame(t, engine, 1)
	}

	if len(ticked) != ticks {
		t.Fatalf("the reader ran %d times over %d ticks", len(ticked), ticks)
	}
	for tick, heard := range ticked {
		if len(heard) != n {
			t.Fatalf("tick %d: the reader heard of %d Entities, want %d: %v", tick, len(heard), n, heard)
		}
		for _, e := range written {
			if heard[e] != "added=false changed=true;" {
				t.Fatalf("tick %d: %v was given %q, want exactly one Changed", tick, e, heard[e])
			}
		}
	}
}

type (
	paddingWriteCmd        kernel.Command[hookRequest, hookResponse]
	paddingReadCmd         kernel.Command[hookRequest, hookResponse]
	paddingImplicitReadCmd kernel.Command[hookRequest, hookResponse]
)

// implicitWide is paddedWide with its padding left implicit: 7 bytes after
// every bool, which Go does not specify the contents of.
type implicitWide struct {
	A bool
	B int64
	C bool
	D int64
	E bool
	F int64
}

// nestedWide holds a paddedWide between two plain fields, so its explicit
// padding sits one struct down.
type nestedWide struct {
	Before int64
	Inner  paddedWide
	After  int64
}

// arrayWide holds an array of paddedWide, so its explicit padding repeats per
// element.
type arrayWide struct {
	Items [2]paddedWide
}

// paddingPlugin owns the four Components the padding proof compares.
type paddingPlugin struct {
	implicit *Store[implicitWide]
	flat     *Store[paddedWide]
	nested   *Store[nestedWide]
	array    *Store[arrayWide]
}

func (p *paddingPlugin) Name() kernel.PluginName { return "padding" }

func (p *paddingPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *paddingPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.implicit = RegisterComponent[implicitWide](registrar, 16)
	p.flat = RegisterComponent[paddedWide](registrar, 16)
	p.nested = RegisterComponent[nestedWide](registrar, 16)
	p.array = RegisterComponent[arrayWide](registrar, 16)
	return nil
}

// fieldOf is a padding proof's one-field *T Query.
type fieldOf[T any] struct{ Row *T }

// scribble fills a stretch of stack with a pattern, so a value built in the
// next call's frame lands on bytes that are not zero.
//
//go:noinline
func scribble() byte {
	var junk [1024]byte
	for i := range junk {
		junk[i] = 0xA5
	}
	sum := byte(0)
	for _, b := range junk {
		sum += b
	}
	return sum
}

// paddedOf builds a padded field by field, in its own frame.
//
//go:noinline
func paddedOf(i int) padded {
	var p padded
	p.Flag = true
	p.Value = int64(i)
	p.Small = 3
	return p
}

// wideOf builds a paddedWide field by field, in its own frame.
//
//go:noinline
func wideOf(i int) paddedWide {
	var p paddedWide
	p.A = true
	p.B = int64(i)
	p.D = int64(2 * i)
	p.E = true
	p.F = 3
	return p
}

//go:noinline
func implicitOf(i int) implicitWide {
	var p implicitWide
	p.A = true
	p.B = int64(i)
	p.D = int64(2 * i)
	p.E = true
	p.F = 3
	return p
}

//go:noinline
func nestedOf(i int) nestedWide {
	var p nestedWide
	p.Before = int64(i)
	p.Inner.A = true
	p.Inner.B = int64(i)
	p.Inner.D = int64(2 * i)
	p.Inner.E = true
	p.Inner.F = 3
	p.After = 7
	return p
}

//go:noinline
func arrayOf(i int) arrayWide {
	var p arrayWide
	for k := range p.Items {
		p.Items[k].A = true
		p.Items[k].B = int64(i + k)
		p.Items[k].D = int64(2 * (i + k))
		p.Items[k].E = true
		p.Items[k].F = 3
	}
	return p
}

// paddingOffsets is every byte of t no named field covers: implicit gaps, and
// blank _ fields, walking nested structs and arrays.
func paddingOffsets(t reflect.Type) []uintptr {
	covered := make([]bool, t.Size())
	var cover func(t reflect.Type, base uintptr)
	cover = func(t reflect.Type, base uintptr) {
		switch t.Kind() {
		case reflect.Struct:
			for i := range t.NumField() {
				if field := t.Field(i); field.Name != "_" {
					cover(field.Type, base+field.Offset)
				}
			}
		case reflect.Array:
			for i := range t.Len() {
				cover(t.Elem(), base+uintptr(i)*t.Elem().Size())
			}
		default:
			for b := range t.Size() {
				covered[base+b] = true
			}
		}
	}
	cover(t, 0)
	var offsets []uintptr
	for b, is := range covered {
		if !is {
			offsets = append(offsets, uintptr(b))
		}
	}
	return offsets
}

// paddingRoute is one way of writing a value equal, field by field, to the one
// an Entity already holds.
type paddingRoute string

const (
	routeFieldBuilt   paddingRoute = "a *T field assigned a built value after a stack filler"
	routeFieldLiteral paddingRoute = "a *T field assigned a literal"
	routeFieldByField paddingRoute = "a *T field written field by field"
	routeRefBuilt     paddingRoute = "Ref assigned a built value after a stack filler"
	routeRefByField   paddingRoute = "Ref written field by field"
	routeUpdateLit    paddingRoute = "UpdateFor with a literal"
	routeUpdateBuilt  paddingRoute = "UpdateFor with a built value after a stack filler"
	routeUpdateOf     paddingRoute = "UpdateFor with the value Of read back"
)

var paddingRoutes = []paddingRoute{
	routeFieldBuilt, routeFieldLiteral, routeFieldByField,
	routeRefBuilt, routeRefByField,
	routeUpdateLit, routeUpdateBuilt, routeUpdateOf,
}

// paddingSubject is one Component under the proof: its handles for the current
// run, and how to build, literally assign and field-by-field write the value
// Entity i holds. Each closure spells the value at its own call site, so a
// literal is a literal there.
type paddingSubject[T any] struct {
	name    string
	store   *Store[T]
	q       *Query[fieldOf[T]]
	set     *Set[T]
	built   func(i int) T
	literal func(p *T, i int)
	update  func(set *Set[T], e Entity, i int)
	fields  func(p *T, i int)
}

// paddingRunner is a paddingSubject with its T forgotten.
type paddingRunner interface {
	add(ids []Entity)
	write(route paddingRoute, ids []Entity, index map[Entity]int)
	changes() int
	dirtyPadding() []string
}

func (s *paddingSubject[T]) add(ids []Entity) {
	for i, e := range ids {
		scribble()
		s.set.UpdateFor(e, s.built(i))
	}
}

func (s *paddingSubject[T]) write(route paddingRoute, ids []Entity, index map[Entity]int) {
	switch route {
	case routeFieldBuilt:
		for e, it := range s.q.All() {
			scribble()
			*it.Row = s.built(index[e])
		}
	case routeFieldLiteral:
		for e, it := range s.q.All() {
			s.literal(it.Row, index[e])
		}
	case routeFieldByField:
		for e, it := range s.q.All() {
			s.fields(it.Row, index[e])
		}
	case routeRefBuilt:
		for i, e := range ids {
			ref, _ := s.set.Ref(e)
			scribble()
			*ref = s.built(i)
		}
	case routeRefByField:
		for i, e := range ids {
			ref, _ := s.set.Ref(e)
			s.fields(ref, i)
		}
	case routeUpdateLit:
		for i, e := range ids {
			s.update(s.set, e, i)
		}
	case routeUpdateBuilt:
		for i, e := range ids {
			scribble()
			s.set.UpdateFor(e, s.built(i))
		}
	case routeUpdateOf:
		for _, e := range ids {
			value, _ := s.set.Of(e)
			s.set.UpdateFor(e, value)
		}
	}
}

func (s *paddingSubject[T]) changes() int {
	if s.store.hooks == nil {
		return 0
	}
	total := 0
	for _, count := range changesIn(s.store.hooks) {
		total += count
	}
	return total
}

// dirtyPadding renders, for every row whose padding holds a non-zero byte, the
// row's padding bytes.
func (s *paddingSubject[T]) dirtyPadding() []string {
	offsets := paddingOffsets(reflect.TypeFor[T]())
	var dirty []string
	for row := range s.store.dense {
		bytes := unsafe.Slice((*byte)(unsafe.Pointer(&s.store.dense[row])), unsafe.Sizeof(s.store.dense[row]))
		padding := make([]byte, len(offsets))
		nonZero := false
		for k, at := range offsets {
			padding[k] = bytes[at]
			nonZero = nonZero || bytes[at] != 0
		}
		if nonZero {
			dirty = append(dirty, fmt.Sprintf("row %d % x", row, padding))
		}
	}
	return dirty
}

// TestExplicitPaddingRecordsNoChangedForEqualFieldValues is the proof hooks.md's
// padding rule rests on: a Component whose padding is spelled as _ [N]byte
// fields records no Changed when it is given values equal field by field to
// what it holds, on every write route, flat, nested one struct down, and in an
// array. The same values in implicitWide, whose padding is implicit, are counted
// beside them for contrast in a release build, and logged rather than asserted:
// there, a Changed no field made is expected on several routes, and its count
// depends on what the route before it left in the padding. A real change is
// counted last, so a count of 0 is not a harness that cannot see one.
func TestExplicitPaddingRecordsNoChangedForEqualFieldValues(t *testing.T) {
	implicit := &paddingSubject[implicitWide]{
		name:    "implicitWide (implicit padding)",
		built:   implicitOf,
		literal: func(p *implicitWide, i int) { *p = implicitWide{A: true, B: int64(i), D: int64(2 * i), E: true, F: 3} },
		update: func(set *Set[implicitWide], e Entity, i int) {
			set.UpdateFor(e, implicitWide{A: true, B: int64(i), D: int64(2 * i), E: true, F: 3})
		},
		fields: func(p *implicitWide, i int) {
			p.A, p.B, p.C, p.D, p.E, p.F = true, int64(i), false, int64(2*i), true, 3
		},
	}
	flat := &paddingSubject[paddedWide]{
		name:    "paddedWide (explicit, flat)",
		built:   wideOf,
		literal: func(p *paddedWide, i int) { *p = paddedWide{A: true, B: int64(i), D: int64(2 * i), E: true, F: 3} },
		update: func(set *Set[paddedWide], e Entity, i int) {
			set.UpdateFor(e, paddedWide{A: true, B: int64(i), D: int64(2 * i), E: true, F: 3})
		},
		fields: func(p *paddedWide, i int) {
			p.A, p.B, p.C, p.D, p.E, p.F = true, int64(i), false, int64(2*i), true, 3
		},
	}
	nested := &paddingSubject[nestedWide]{
		name:  "nestedWide (explicit, nested)",
		built: nestedOf,
		literal: func(p *nestedWide, i int) {
			*p = nestedWide{Before: int64(i), Inner: paddedWide{A: true, B: int64(i), D: int64(2 * i), E: true, F: 3}, After: 7}
		},
		update: func(set *Set[nestedWide], e Entity, i int) {
			set.UpdateFor(e, nestedWide{Before: int64(i), Inner: paddedWide{A: true, B: int64(i), D: int64(2 * i), E: true, F: 3}, After: 7})
		},
		fields: func(p *nestedWide, i int) {
			p.Before, p.After = int64(i), 7
			p.Inner.A, p.Inner.B, p.Inner.C, p.Inner.D, p.Inner.E, p.Inner.F = true, int64(i), false, int64(2*i), true, 3
		},
	}
	array := &paddingSubject[arrayWide]{
		name:  "arrayWide (explicit, array)",
		built: arrayOf,
		literal: func(p *arrayWide, i int) {
			*p = arrayWide{Items: [2]paddedWide{
				{A: true, B: int64(i), D: int64(2 * i), E: true, F: 3},
				{A: true, B: int64(i + 1), D: int64(2 * (i + 1)), E: true, F: 3},
			}}
		},
		update: func(set *Set[arrayWide], e Entity, i int) {
			set.UpdateFor(e, arrayWide{Items: [2]paddedWide{
				{A: true, B: int64(i), D: int64(2 * i), E: true, F: 3},
				{A: true, B: int64(i + 1), D: int64(2 * (i + 1)), E: true, F: 3},
			}})
		},
		fields: func(p *arrayWide, i int) {
			for k := range p.Items {
				item := &p.Items[k]
				item.A, item.B, item.C, item.D, item.E, item.F = true, int64(i+k), false, int64(2*(i+k)), true, 3
			}
		},
	}
	runners := []paddingRunner{implicit, flat, nested, array}
	names := []string{implicit.name, flat.name, nested.name, array.name}

	var step func()
	owner := &paddingPlugin{}
	entities, _, engine := newWorldWith(t, 16, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[paddingWriteCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(qi *Query[fieldOf[implicitWide]], si *Set[implicitWide], qf *Query[fieldOf[paddedWide]], sf *Set[paddedWide],
				qn *Query[fieldOf[nestedWide]], sn *Set[nestedWide], qa *Query[fieldOf[arrayWide]], sa *Set[arrayWide],
			) {
				implicit.q, implicit.set = qi, si
				flat.q, flat.set = qf, sf
				nested.q, nested.set = qn, sn
				array.q, array.set = qa, sa
				step()
			}))
		registrar.HandleCommand[paddingReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(*Hooks[paddedWide, HookAddedChanged],
				*Hooks[nestedWide, HookAddedChanged], *Hooks[arrayWide, HookAddedChanged],
			) {
			}))
		// A validating build refuses a Changed reader of implicitWide at
		// registration, which is the rule this test's contrast shows the reason
		// for, so the contrast is counted in a release build only.
		if !validate {
			registrar.HandleCommand[paddingImplicitReadCmd](ToExecute[hookRequest, hookResponse](registrar,
				func(*Hooks[implicitWide, HookAddedChanged]) {}))
		}
	}, []kernel.PluginName{Name, "components", "padding"}, owner)
	implicit.store, flat.store, nested.store, array.store = owner.implicit, owner.flat, owner.nested, owner.array

	execute := func(what string, f func()) []int {
		t.Helper()
		step = f
		if _, err := engine.Executioner().ExecuteCommand[paddingWriteCmd](hookRequest{}); err != nil {
			t.Fatalf("%s: running the writer: %v", what, err)
		}
		counts := make([]int, len(runners))
		for k, runner := range runners {
			counts[k] = runner.changes()
		}
		if _, err := engine.Executioner().ExecuteCommand[paddingReadCmd](hookRequest{}); err != nil {
			t.Fatalf("%s: running the reader: %v", what, err)
		}
		if !validate {
			if _, err := engine.Executioner().ExecuteCommand[paddingImplicitReadCmd](hookRequest{}); err != nil {
				t.Fatalf("%s: running the implicit reader: %v", what, err)
			}
		}
		return counts
	}

	ids := make([]Entity, 8)
	index := map[Entity]int{}
	for i := range ids {
		ids[i] = entities.alloc()
		index[ids[i]] = i
	}
	execute("the additions", func() {
		for _, runner := range runners {
			runner.add(ids)
		}
	})
	for _, route := range paddingRoutes {
		counts := execute(string(route), func() {
			for _, runner := range runners {
				runner.write(route, ids, index)
			}
		})
		for k, runner := range runners {
			if k == 0 {
				if !validate {
					t.Logf("%-52s %-32s Changed %d", route, names[k], counts[k])
				}
				continue
			}
			t.Logf("%-52s %-32s Changed %d", route, names[k], counts[k])
			if counts[k] != 0 {
				t.Errorf("%s: %s recorded %d Changed for equal field values, want 0", route, names[k], counts[k])
			}
			for _, dirty := range runner.dirtyPadding() {
				t.Errorf("%s: %s holds non-zero explicit padding: %s", route, names[k], dirty)
			}
		}
		if dirty := implicit.dirtyPadding(); !validate && len(dirty) > 0 {
			t.Logf("%-52s %-32s padding bytes: %s", route, names[0], dirty[0])
		}
	}

	counts := execute("a real change", func() {
		ref, _ := flat.set.Ref(ids[1])
		ref.F = 4
		nestedRef, _ := nested.set.Ref(ids[1])
		nestedRef.Inner.F = 4
		arrayRef, _ := array.set.Ref(ids[1])
		arrayRef.Items[1].F = 4
	})
	if counts[1] != 1 || counts[2] != 1 || counts[3] != 1 {
		t.Fatalf("a real change recorded %v Changed on the explicit Components, want 1 each", counts[1:])
	}
}

// TestRecordingAChangeAllocatesNothingAfterTheFirstWatchedRun is hooks.md §
// How recording is switched on: a writer's row copies are empty until its first
// watched run, which allocates them, and every later run of Ref, UpdateFor and
// a *T field on a watched Store allocates nothing.
func TestRecordingAChangeAllocatesNothingAfterTheFirstWatchedRun(t *testing.T) {
	var set *Set[collider]
	var query *Query[colliderQuery]
	var reader *Hooks[collider, HookAddedChanged]
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar, func(s *Set[collider], q *Query[colliderQuery]) {
			set, query = s, q
		}))
		registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, func(h *Hooks[collider, HookAddedChanged]) {
			reader = h
		}))
	})
	ids := make([]Entity, 64)
	for i := range ids {
		ids[i] = entities.alloc()
		components.colliders.Set(ids[i], collider{})
	}
	frame(t, engine, 1)
	copied := set.changes
	if query.copies[0] != copied {
		t.Fatal("a Set and a *T field in one System keep two row copies of one Store")
	}
	if copied.owners != nil || copied.rows != nil || copied.stamps != nil {
		t.Fatal("the row copy was allocated before its first watched run")
	}

	delivered := 0
	value := float32(0)
	run := func(write func()) func() {
		return func() {
			value++
			write()
			copied.compare()
			reader.beginRun()
			// Counted off the copy rather than ranged: a range over All inside
			// this nested closure allocates its yield, which is the harness's
			// cost and not the path under test.
			delivered += len(reader.out)
			reader.endRun()
		}
	}
	for _, arm := range []struct {
		name  string
		write func()
	}{
		{"Ref", func() {
			for _, e := range ids {
				ref, _ := set.Ref(e)
				ref.Radius = value
			}
		}},
		{"UpdateFor, replacing", func() {
			for _, e := range ids {
				set.UpdateFor(e, collider{Radius: value})
			}
		}},
		{"a *T field", func() {
			for _, it := range query.All() {
				it.Collider.Radius = value
			}
		}},
	} {
		delivered = 0
		first := allocationsDuring(run(arm.write))
		if arm.name == "Ref" && (first == 0 || cap(copied.rows) == 0 || cap(copied.stamps) == 0) {
			t.Fatalf("the first watched run allocated %d objects and left rows with capacity %d, want its row copies allocated",
				first, cap(copied.rows))
		}
		if objects := testing.AllocsPerRun(100, run(arm.write)); objects != 0 {
			t.Fatalf("%s on a watched Store allocated %v objects a run after the first, want 0", arm.name, objects)
		}
		if runs := 1 + 101; delivered != runs*len(ids) {
			t.Fatalf("%s: the reader was given %d Changed over %d runs of %d Entities, want one per Entity per run",
				arm.name, delivered, runs, len(ids))
		}
	}
}
