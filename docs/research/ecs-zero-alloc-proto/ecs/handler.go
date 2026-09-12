package ecs

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// paramLike is every legal System parameter that has to be bound at
// registration and refreshed per tick. The builder reaches all of them through
// this one interface, which is what lets the reflection stay generic-free.
type paramLike interface {
	plan(a kernel.ResourceAccess, en *Entities)
	refresh()
}

// WriteableEntities is the System parameter that grants Despawn. It takes
// write{*Entities}, which under cog#240 is the single lock covering every Store.
type WriteableEntities struct {
	resolve func() *Entities
	en      *Entities
}

func (w *WriteableEntities) plan(a kernel.ResourceAccess, _ *Entities) {
	h := a.GetWrite[*Entities]()
	w.resolve = func() *Entities { return h.Get() }
}

func (w *WriteableEntities) refresh()              { w.en = w.resolve() }
func (w *WriteableEntities) Despawn(e Entity) bool { return w.en.Despawn(e) }
func (w *WriteableEntities) Live() int             { return w.en.Live() }

// Spawn creates Entities with the complete Component set named by the bundle
// struct B. It is a handle, not a Command: cog#240 measured a Uses dispatch at
// 1113ns against 26ns for a bundle scattered through closures cached at
// registration, a factor of 2270, all of it the scheduler round-trip.
type Spawn[B any] struct {
	resolve func() *Entities
	en      *Entities
	adds    []func(e Entity, src unsafe.Pointer)
	offs    []uintptr

	// buf holds the bundle for the duration of New. It is a field rather than a
	// local because taking the address of a local parameter and handing it to an
	// indirect call forces that local to the heap -- one allocation per spawn.
	// Spawn is already heap-allocated, once, at registration.
	buf B
}

func (s *Spawn[B]) plan(a kernel.ResourceAccess, en *Entities) {
	h := a.GetWrite[*Entities]()
	s.resolve = func() *Entities { return h.Get() }
	t := reflect.TypeFor[B]()
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("ecs: Spawn bundle %s is not a struct", t))
	}
	for i := range t.NumField() {
		sf := t.Field(i)
		ops, ok := en.ops[sf.Type]
		if !ok {
			panic(fmt.Sprintf("ecs: Spawn bundle %s names unregistered Component %s", t, sf.Type))
		}
		// A spawn names its Components, so its lock set is knowable here.
		ops.declareWrite(a)
		s.adds = append(s.adds, ops.addRaw)
		s.offs = append(s.offs, sf.Offset)
	}
}

func (s *Spawn[B]) refresh() { s.en = s.resolve() }

func (s *Spawn[B]) New(b B) Entity {
	s.buf = b
	p := unsafe.Pointer(&s.buf)
	e := s.en.Alloc()
	for i, add := range s.adds {
		add(e, unsafe.Add(p, s.offs[i]))
	}
	return e
}

// ToHandler turns a plain Go func into the (Lock, Observe) pair an ordinary cog
// subscription already returns, so Before/After/First/Last, ownership and
// Describe all work unchanged and the ECS contributes no registration API of
// its own (cog#238).
//
// Every parameter is classified once, here. The call itself goes through
// reflect.Value.Call, which cog#234 measured at 0 allocs/op for arity 0-12 --
// provided nothing is re-boxed per call and the callee returns nothing. Both
// conditions are enforced: a System returning anything is rejected, and the
// event and Kernel are written through stable pointer cells rather than boxed.
//
// The event parameter is optional, and leaving it out is the better default: a
// System that names E can only ever be subscribed to E. Feed projects what the
// System actually needs out of the event instead, so the System stays usable
// under any event that can supply it.
func ToHandler[E any](
	en *Entities, system any, feeds ...Feeder,
) func() (kernel.Lock, kernel.Observe[E]) {
	return func() (kernel.Lock, kernel.Observe[E]) {
		fnv := reflect.ValueOf(system)
		ft := fnv.Type()
		if ft.Kind() != reflect.Func {
			panic("ecs: a System must be a func")
		}
		if ft.NumOut() != 0 {
			panic("ecs: a System returns nothing; reflect.Value.Call allocates for a callee that returns a value (cog#234)")
		}

		evType := reflect.TypeFor[E]()
		kType := reflect.TypeFor[kernel.Kernel]()
		args := make([]reflect.Value, ft.NumIn())
		params := make([]paramLike, 0, ft.NumIn())
		evp := new(E)
		kp := new(kernel.Kernel)

		for i := range ft.NumIn() {
			pt := ft.In(i)
			switch {
			case pt == evType:
				// Addressable and stable: the tick writes *evp, so no per-call
				// boxing of a non-pointer-shaped struct ever happens.
				args[i] = reflect.ValueOf(evp).Elem()
			case pt == kType:
				args[i] = reflect.ValueOf(kp).Elem()
			case feederFor(feeds, pt) >= 0:
				// The In instance comes from the Feeder rather than from
				// reflect.New, because the typed closure that fills it was
				// already bound to that instance at registration.
				args[i] = feeds[feederFor(feeds, pt)].val
			case pt.Kind() == reflect.Pointer:
				obj := reflect.New(pt.Elem())
				p, ok := obj.Interface().(paramLike)
				if !ok {
					panic(fmt.Sprintf("ecs: %s is not a legal System parameter", pt))
				}
				args[i] = obj
				params = append(params, p)
			default:
				panic(fmt.Sprintf("ecs: %s is not a legal System parameter", pt))
			}
		}

		lock := func(a kernel.ResourceAccess) {
			// Unconditional and first: every handler that touches any Store
			// declares read{*Entities}. A Spawn or Despawn parameter upgrades it
			// to a write, and the kernel lets a write supersede a read.
			a.GetRead[*Entities]()
			for _, p := range params {
				p.plan(a, en)
			}
		}
		observe := func(k kernel.Kernel, ev E) error {
			*evp = ev
			*kp = k
			// The feeds run off the same stable cell the event parameter would
			// have read, so projecting costs one indirect call and no boxing.
			for _, f := range feeds {
				f.feed(unsafe.Pointer(evp))
			}
			for _, p := range params {
				p.refresh()
			}
			fnv.Call(args)
			return nil
		}
		return lock, observe
	}
}

// feederFor is the index of the Feeder that supplies parameter type pt, or -1.
// A linear scan over a list that is empty or one long, walked once per
// parameter at registration.
func feederFor(feeds []Feeder, pt reflect.Type) int {
	for i := range feeds {
		if feeds[i].typ == pt {
			return i
		}
	}
	return -1
}

// ToHandlerHybrid keeps every part of ToHandler except the call: the same
// reflection over the signature, the same reflect.New parameter construction,
// the same paramLike slice, the same stable event and Kernel cells. Only
// fnv.Call is replaced by a direct call.
//
// It exists to attribute the whole-frame gap between ToHandler and ToHandler1.
// If this lands next to ToHandler1 the gap is the reflected call; if it lands
// next to ToHandler the gap is the machinery around it.
func ToHandlerHybrid[E any, Q0 any](
	en *Entities, fn func(*Query[Q0], E),
) func() (kernel.Lock, kernel.Observe[E]) {
	return func() (kernel.Lock, kernel.Observe[E]) {
		ft := reflect.TypeOf(fn)
		obj := reflect.New(ft.In(0).Elem())
		q0 := obj.Interface().(*Query[Q0])
		params := []paramLike{q0}
		evp := new(E)
		kp := new(kernel.Kernel)
		_ = reflect.ValueOf(evp).Elem()

		lock := func(a kernel.ResourceAccess) {
			a.GetRead[*Entities]()
			for _, p := range params {
				p.plan(a, en)
			}
		}
		observe := func(k kernel.Kernel, ev E) error {
			*evp = ev
			*kp = k
			for _, p := range params {
				p.refresh()
			}
			fn(q0, *evp)
			return nil
		}
		return lock, observe
	}
}

// ToHandler1 is the same builder with the call baked instead of reflected: Go
// infers both type parameters from the func literal, so the System still reads
// as a plain func, but there is one name per arity. It exists to price the
// reflected call against the direct one, which is the only difference between
// the two paths.
func ToHandler1[E any, Q0 any](en *Entities, fn func(*Query[Q0], E)) func() (kernel.Lock, kernel.Observe[E]) {
	return func() (kernel.Lock, kernel.Observe[E]) {
		q0 := &Query[Q0]{}
		return func(a kernel.ResourceAccess) {
				a.GetRead[*Entities]()
				q0.plan(a, en)
			}, func(_ kernel.Kernel, ev E) error {
				q0.refresh()
				fn(q0, ev)
				return nil
			}
	}
}

// ToHandler2 is ToHandler1 at two Queries.
func ToHandler2[E any, Q0 any, Q1 any](
	en *Entities, fn func(*Query[Q0], *Query[Q1], E),
) func() (kernel.Lock, kernel.Observe[E]) {
	return func() (kernel.Lock, kernel.Observe[E]) {
		q0, q1 := &Query[Q0]{}, &Query[Q1]{}
		return func(a kernel.ResourceAccess) {
				a.GetRead[*Entities]()
				q0.plan(a, en)
				q1.plan(a, en)
			}, func(_ kernel.Kernel, ev E) error {
				q0.refresh()
				q1.refresh()
				fn(q0, q1, ev)
				return nil
			}
	}
}
