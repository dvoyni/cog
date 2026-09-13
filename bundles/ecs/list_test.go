package ecs

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

type labelled struct {
	Name string
}

type inventory struct {
	Slots List[uint32]
}

func TestStorableAdmitsImmutableIndirectionAndNothingElse(t *testing.T) {
	type deep struct {
		Label   string
		Nested  struct{ Also string }
		Labels  [3]string
		Numbers List[float32]
		Count   int32
	}
	legal := []struct {
		name string
		t    reflect.Type
	}{
		{"a string", reflect.TypeFor[labelled]()},
		{"a list", reflect.TypeFor[inventory]()},
		{"strings and lists several structs down", reflect.TypeFor[deep]()},
		{"everything the pointer-free rule already allowed", reflect.TypeFor[position]()},
		{"a static blob", reflect.TypeFor[pixels]()},
		{"a list of a struct holding a list", reflect.TypeFor[grid]()},
	}
	for _, test := range legal {
		if err := Storable(test.t); err != nil {
			t.Fatalf("Storable(%s) = %v, want nil", test.name, err)
		}
	}

	illegal := []struct {
		kind string
		t    reflect.Type
	}{
		{"a pointer", reflect.TypeFor[*inner]()},
		{"a bare slice", reflect.TypeFor[[]uint32]()},
		{"a bare byte slice", reflect.TypeFor[[]byte]()},
		{"a pointer to a blob", reflect.TypeFor[*m.Blob]()},
		{"a map", reflect.TypeFor[map[string]int]()},
		{"an interface", reflect.TypeFor[any]()},
		{"a channel", reflect.TypeFor[chan int]()},
		{"a struct several levels above a pointer", reflect.TypeFor[cage]()},
	}
	for _, test := range illegal {
		if err := Storable(test.t); err == nil {
			t.Fatalf("Storable(%s) = nil, want a refusal", test.kind)
		}
	}
}

// TestABareSliceIsRefusedForTheLockUnitAndSaysSo is the rule that matters most
// and the one a reader is most likely to want to argue with, so the refusal has
// to carry its reason rather than just its verdict.
func TestABareSliceIsRefusedForTheLockUnitAndSaysSo(t *testing.T) {
	type hasSlice struct {
		Items []uint32
	}
	err := Storable(reflect.TypeFor[hasSlice]())
	if err == nil {
		t.Fatal("a Component holding a []T was accepted")
	}
	if !strings.Contains(err.Error(), "ecs.List") {
		t.Fatalf("the refusal does not name the answer: %v", err)
	}
	if !strings.Contains(err.Error(), "hasSlice.Items") {
		t.Fatalf("the refusal does not name the field by path: %v", err)
	}
}

// pixels holds a Blob, which is the one slice a Component may hold outright:
// it is admitted by type identity, on the contract that its bytes are never
// written after construction.
type pixels struct {
	Width, Height int32
	Bytes         m.Blob
}

// row and grid are a List whose element type holds a List, which validation
// reaches by walking the outer List's elements.
type row struct {
	Cells List[uint32]
}

type grid struct {
	Rows List[row]
}

// TestPointerFreeStillMeansWhatItMeant is what keeps the relaxation from
// quietly widening the fast path: a string and a List are legal Components and
// are not pointer-free, and everything downstream keys off that distinction.
func TestPointerFreeStillMeansWhatItMeant(t *testing.T) {
	for _, tp := range []reflect.Type{
		reflect.TypeFor[labelled](), reflect.TypeFor[inventory](), reflect.TypeFor[pixels](),
	} {
		if err := Storable(tp); err != nil {
			t.Fatalf("%s is not storable: %v", tp, err)
		}
		if err := PointerFree(tp); err == nil {
			t.Fatalf("%s is reported pointer-free, so it would take the barrier-free fill", tp)
		}
	}
}

func TestAListCopiesRatherThanAdoptingWhatItWasBuiltFrom(t *testing.T) {
	source := []uint32{1, 2, 3}
	list := ListOf(source)
	source[0] = 99
	if got := list.At(0); got != 1 {
		t.Fatalf("the List saw the caller's write: element 0 is %d, want 1", got)
	}
	if list.Len() != 3 {
		t.Fatalf("Len is %d, want 3", list.Len())
	}
	if empty := ListOf[uint32](nil); empty.Len() != 0 {
		t.Fatalf("the zero List has length %d, want 0", empty.Len())
	}
	seen := 0
	for i, value := range NewList[uint32](7, 8).All() {
		if value != uint32(7+i) {
			t.Fatalf("element %d is %d", i, value)
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("All yielded %d elements, want 2", seen)
	}
}

// TestARemovedRowDoesNotKeepItsValueAlive is the reason remove zeroes a
// non-trivial row. Without it the vacated slot holds the last owner's bytes
// until something else happens to take the row, so a despawned Entity's data
// outlives it by an unbounded time.
//
// It is written with a finaliser because that is the only way to ask the
// question the fix is about — reachability — rather than a proxy for it.
func TestARemovedRowDoesNotKeepItsValueAlive(t *testing.T) {
	if validate {
		// Validation mode's stamp table is keyed by the backing array's own
		// pointer, so an entry keeps that array alive until it is evicted. That
		// is a property of the mode rather than of the Store, and it is stated
		// in validate_on.go; asserting reachability against it would be
		// asserting the wrong thing.
		t.Skip("validation mode retains stamped arrays by design")
	}
	entities := newEntities(8)
	store := NewStore[inventory](entities, 8)

	collected := make(chan struct{}, 1)
	func() {
		e := entities.alloc()
		slots := ListOf([]uint32{1, 2, 3})
		runtime.SetFinalizer(&slots.data[0], func(*uint32) { collected <- struct{}{} })
		store.Set(e, inventory{Slots: slots})
		store.Remove(e)
	}()

	// The Store has to be kept alive by hand. Nothing below reads it, so Go's
	// liveness analysis would retire it at the last use above and collect the
	// whole dense array with it — which passes this test whether or not remove
	// clears anything, and is exactly the shape of a test that proves nothing.
	for range 5 {
		runtime.GC()
		select {
		case <-collected:
			runtime.KeepAlive(store)
			return
		default:
		}
	}
	runtime.KeepAlive(store)
	t.Fatal("the backing array of a removed row is still reachable, so the Store is holding a despawned Entity's data")
}

// TestATrivialRowIsLeftWhereItLies pins the other half of the same decision:
// the zeroing is for the rows that need it and nothing else, because a
// pointer-free row keeps nothing alive and clearing it would be work for no
// one.
func TestATrivialRowIsLeftWhereItLies(t *testing.T) {
	entities := newEntities(8)
	trivial := NewStore[position](entities, 8)
	if !trivial.trivial {
		t.Fatal("a pointer-free Component is not marked trivial, so removal would zero rows for nothing")
	}
	rich := NewStore[labelled](entities, 8)
	if rich.trivial {
		t.Fatal("a Component holding a string is marked trivial, so its removed rows would not be cleared")
	}
}

// richPlugin owns the Components that are legal but not pointer-free. It is its
// own plugin rather than a field on componentsPlugin because every other test
// in this package counts Store lengths to reason about driver selection, and a
// Component nobody asked for would change what they measure.
type richPlugin struct {
	ids         uint32
	labels      *Store[labelled]
	inventories *Store[inventory]
}

func (p *richPlugin) Name() kernel.PluginName { return "rich" }

func (p *richPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *richPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.labels = RegisterComponent[labelled](registrar, p.ids)
	p.inventories = RegisterComponent[inventory](registrar, p.ids)
	return nil
}

type labelQuery struct {
	Label labelled
}

type richSystem kernel.Subscription[app.UpdateEvent]

// TestANonTrivialQueryTakesTheTypedCopyAndTheWideShape pins the routing, which
// is the decision most likely to be undone by someone tidying prepare. A Query
// naming a Component that is not pointer-free must not take an unrolled filler:
// those call fill, fill writes a row's bytes through an unsafe.Pointer with no
// write barrier, and for a row holding a string that is a pointer store the
// collector never sees.
func TestANonTrivialQueryTakesTheTypedCopyAndTheWideShape(t *testing.T) {
	var query *Query[labelQuery]
	var read string
	rich := &richPlugin{ids: 16}
	entities, _, engine := newWorldWith(t, 16,
		func(registrar *kernel.Registrar) {
			registrar.Subscribe[richSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[labelQuery]) {
				query = q
				for _, it := range q.All() {
					read = it.Label.Name
				}
			}))
		}, []kernel.PluginName{Name, "components", "rich"}, rich)

	rich.labels.Set(entities.alloc(), labelled{Name: "a name the fill has to copy properly"})
	if err := engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
		t.Fatalf("publishing the update: %v", err)
	}

	if read != "a name the fill has to copy properly" {
		t.Fatalf("the Query read %q", read)
	}
	if query.shape != wideShape {
		t.Fatalf("a Query over a Component holding a string took shape %d, want the per-field loop at %d",
			query.shape, wideShape)
	}
	if query.fields[0].copy == nil {
		t.Fatal("the field planned no typed copy, so the fill would write its pointer without a barrier")
	}
}

// TestATrivialQueryIsUntouched is the other half of the routing, and the reason
// it was chosen: every Query that was legal before string and List arrived
// keeps the filler it was measured with.
func TestATrivialQueryIsUntouched(t *testing.T) {
	query, _, _, _ := capture(t, 32)
	if query.shape == wideShape {
		t.Fatal("a pointer-free Query was routed to the per-field loop")
	}
	for i := range query.fields {
		if query.fields[i].copy != nil {
			t.Fatalf("field %d of a pointer-free Query planned a typed copy", i)
		}
	}
}
