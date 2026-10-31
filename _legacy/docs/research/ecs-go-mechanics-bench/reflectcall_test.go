package bench

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/docs/research/ecs-go-mechanics-bench/store"
)

// ---- the "systems" under test ----

type Q1 struct{ s *store.Store }
type Q2 struct{ s *store.Store }
type Q3 struct{ s *store.Store }
type Q4 struct{ s *store.Store }

var RSink float32

func sys0()                           { RSink++ }
func sys1(a Q1)                       { RSink += float32(a.s.Len()) }
func sys2(a Q1, b Q2)                 { RSink += float32(a.s.Len() + b.s.Len()) }
func sys3(a Q1, b Q2, c Q3)           { RSink += float32(a.s.Len() + b.s.Len() + c.s.Len()) }
func sys4(a Q1, b Q2, c Q3, d Q4)     { RSink += float32(a.s.Len() + b.s.Len() + c.s.Len() + d.s.Len()) }
func sysPtr(a *store.Store)           { RSink += float32(a.Len()) }
func sysBig(a Big)                    { RSink += a.F[0] }
func sysRet(a Q1) float32             { return float32(a.s.Len()) }
func sys8(a, b, c, d, e, f, g, h int) { RSink += float32(a + b + c + d + e + f + g + h) }

type Big struct{ F [128]float32 }

// ---- baseline: a direct typed closure call ----

func BenchmarkCallDirectTyped(b *testing.B) {
	s := store.New(N)
	q1, q2, q3 := Q1{s}, Q2{s}, Q3{s}
	fn := func() { sys3(q1, q2, q3) }
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		fn()
	}
}

// ---- baseline: an interface method call (the "registered instantiation" path) ----

type runner interface{ Run() }

type boundSys3 struct {
	fn func(Q1, Q2, Q3)
	a  Q1
	c  Q2
	d  Q3
}

func (s *boundSys3) Run() { s.fn(s.a, s.c, s.d) }

func BenchmarkCallViaInterface(b *testing.B) {
	s := store.New(N)
	var r runner = &boundSys3{fn: sys3, a: Q1{s}, c: Q2{s}, d: Q3{s}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		r.Run()
	}
}

// ---- reflect.Value.Call, arity sweep, args rebuilt every call ----

func benchReflectCall(b *testing.B, fn any, args []reflect.Value) {
	v := reflect.ValueOf(fn)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v.Call(args)
	}
}

func BenchmarkReflectCall0(b *testing.B) { benchReflectCall(b, sys0, nil) }

func BenchmarkReflectCall1(b *testing.B) {
	s := store.New(N)
	benchReflectCall(b, sys1, []reflect.Value{reflect.ValueOf(Q1{s})})
}

func BenchmarkReflectCall2(b *testing.B) {
	s := store.New(N)
	benchReflectCall(b, sys2, []reflect.Value{reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s})})
}

func BenchmarkReflectCall3(b *testing.B) {
	s := store.New(N)
	benchReflectCall(b, sys3, []reflect.Value{
		reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s})})
}

func BenchmarkReflectCall4(b *testing.B) {
	s := store.New(N)
	benchReflectCall(b, sys4, []reflect.Value{
		reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s}), reflect.ValueOf(Q4{s})})
}

func BenchmarkReflectCall8(b *testing.B) {
	args := make([]reflect.Value, 8)
	for i := range args {
		args[i] = reflect.ValueOf(i)
	}
	benchReflectCall(b, sys8, args)
}

// A pointer argument: pointer-shaped, so no boxing allocation at ValueOf time.
func BenchmarkReflectCallPtrArg(b *testing.B) {
	s := store.New(N)
	benchReflectCall(b, sysPtr, []reflect.Value{reflect.ValueOf(s)})
}

// A large struct argument.
func BenchmarkReflectCallBigArg(b *testing.B) {
	benchReflectCall(b, sysBig, []reflect.Value{reflect.ValueOf(Big{})})
}

// A function with a return value: Call must allocate the results slice.
func BenchmarkReflectCallWithResult(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sysRet)
	args := []reflect.Value{reflect.ValueOf(Q1{s})}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		out := v.Call(args)
		RSink = float32(out[0].Float())
	}
}

// ---- reflect.Value.Call with args BOXED PER CALL (the honest worst case) ----

func BenchmarkReflectCallBoxPerCall(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sys3)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		args := []reflect.Value{reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s})}
		v.Call(args)
	}
}

// ---- reflect.MakeFunc ----

// MakeFunc builds a func of a runtime-known type whose body is a reflective
// callback. Calling it goes through reflect's callReflect trampoline.
func BenchmarkMakeFuncCall(b *testing.B) {
	s := store.New(N)
	t := reflect.TypeOf(sys3)
	mf := reflect.MakeFunc(t, func(in []reflect.Value) []reflect.Value {
		RSink += float32(in[0].Interface().(Q1).s.Len())
		return nil
	})
	typed := mf.Interface().(func(Q1, Q2, Q3))
	q1, q2, q3 := Q1{s}, Q2{s}, Q3{s}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		typed(q1, q2, q3)
	}
}

// MakeFunc whose body ignores its args entirely, to isolate the trampoline.
func BenchmarkMakeFuncCallEmptyBody(b *testing.B) {
	s := store.New(N)
	t := reflect.TypeOf(sys3)
	mf := reflect.MakeFunc(t, func(in []reflect.Value) []reflect.Value {
		RSink++
		return nil
	})
	typed := mf.Interface().(func(Q1, Q2, Q3))
	q1, q2, q3 := Q1{s}, Q2{s}, Q3{s}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		typed(q1, q2, q3)
	}
}

// ---- the bake: reflect at registration, typed closure per invocation ----

// bake3 is what a registration-time adapter builder can do WITHOUT generics: a
// type switch over the concrete func type recovers a typed call. It works only
// for func types the switch names literally.
func bake3(fn any, s *store.Store) func() {
	switch f := fn.(type) {
	case func(Q1, Q2, Q3):
		a, c, d := Q1{s}, Q2{s}, Q3{s}
		return func() { f(a, c, d) }
	}
	// Fallback: reflection per invocation.
	v := reflect.ValueOf(fn)
	args := []reflect.Value{reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s})}
	return func() { v.Call(args) }
}

func BenchmarkBakedTypeSwitch(b *testing.B) {
	s := store.New(N)
	run := bake3(sys3, s)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		run()
	}
}

func BenchmarkBakedFallback(b *testing.B) {
	s := store.New(N)
	// sys4 is not named by the switch, so it lands on the reflective fallback.
	run := bake4Fallback(sys4, s)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		run()
	}
}

func bake4Fallback(fn any, s *store.Store) func() {
	v := reflect.ValueOf(fn)
	args := []reflect.Value{
		reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s}), reflect.ValueOf(Q4{s})}
	return func() { v.Call(args) }
}

// ---- the fixed-arity generic fallback, sketched and measured ----

// Query is what a generic registration constructor would bind. Building it is
// the generic work; reflect.TypeFor is registration-time only.
type Query[T any] struct{ s *store.Store }

func bindQuery[T any](s *store.Store) (Query[T], reflect.Type) {
	return Query[T]{s: s}, reflect.TypeFor[T]()
}

// System3 is the fixed-arity generic constructor. Its type parameters are
// compile-time, supplied by the CALL SITE, so no reflect.Type ever has to be
// turned back into a type argument.
func System3[A, B, C any](fn func(Query[A], Query[B], Query[C]), s *store.Store) (func(), []reflect.Type) {
	qa, ta := bindQuery[A](s)
	qb, tb := bindQuery[B](s)
	qc, tc := bindQuery[C](s)
	return func() { fn(qa, qb, qc) }, []reflect.Type{ta, tb, tc}
}

func genericSys3(a Query[store.Body], b Query[Big], c Query[int]) {
	RSink += float32(a.s.Len())
}

func BenchmarkGenericFixedArity3(b *testing.B) {
	s := store.New(N)
	run, locks := System3(genericSys3, s)
	if len(locks) != 3 {
		b.Fatal("expected 3 lock types")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		run()
	}
}

// ---- where reflect.Value.Call DOES allocate ----

func sysFloat(a float32, b Big) { RSink += a + b.F[0] }

// A non-pointer-shaped argument whose value changes per call cannot be hoisted,
// so reflect.ValueOf must box it into an interface every time.
func BenchmarkReflectCallBoxNonPointerPerCall(b *testing.B) {
	v := reflect.ValueOf(sysFloat)
	args := make([]reflect.Value, 2)
	var big Big
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		big.F[0] = float32(i)
		args[0] = reflect.ValueOf(float32(i))
		args[1] = reflect.ValueOf(big)
		v.Call(args)
	}
}

// Only the small scalar changes; the big struct is boxed once.
func BenchmarkReflectCallBoxScalarPerCall(b *testing.B) {
	v := reflect.ValueOf(sysFloat)
	args := []reflect.Value{reflect.ValueOf(float32(0)), reflect.ValueOf(Big{})}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		args[0] = reflect.ValueOf(float32(i))
		v.Call(args)
	}
}

// The registration-time shape: box every argument ONCE, mutate the underlying
// data through a pointer, and reuse the same []reflect.Value forever.
func BenchmarkReflectCallPreboxedReused(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sys3)
	args := []reflect.Value{reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s})}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Call(args)
	}
}

// Variadic-free comparison at higher arity, all pointer-shaped.
func sys12(a, b, c, d, e, f, g, h, i, j, k, l *store.Store) { RSink += float32(a.Len()) }

func BenchmarkReflectCall12(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sys12)
	args := make([]reflect.Value, 12)
	for i := range args {
		args[i] = reflect.ValueOf(s)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Call(args)
	}
}

// ---- arity reach of the fixed-arity generic fallback ----

// System8 shows that type inference scales: the call site writes System8(fn, s)
// with no explicit type arguments and the compiler infers all eight.
func System8[A, B, C, D, E, F, G, H any](
	fn func(Query[A], Query[B], Query[C], Query[D], Query[E], Query[F], Query[G], Query[H]),
	s *store.Store,
) (func(), []reflect.Type) {
	qa, ta := bindQuery[A](s)
	qb, tb := bindQuery[B](s)
	qc, tc := bindQuery[C](s)
	qd, td := bindQuery[D](s)
	qe, te := bindQuery[E](s)
	qf, tf := bindQuery[F](s)
	qg, tg := bindQuery[G](s)
	qh, th := bindQuery[H](s)
	return func() { fn(qa, qb, qc, qd, qe, qf, qg, qh) },
		[]reflect.Type{ta, tb, tc, td, te, tf, tg, th}
}

type C1 struct{ v float32 }
type C2 struct{ v float32 }
type C3 struct{ v float32 }
type C4 struct{ v float32 }
type C5 struct{ v float32 }
type C6 struct{ v float32 }
type C7 struct{ v float32 }
type C8 struct{ v float32 }

func genericSys8(a Query[C1], b Query[C2], c Query[C3], d Query[C4],
	e Query[C5], f Query[C6], g Query[C7], h Query[C8]) {
	RSink += float32(a.s.Len())
}

func BenchmarkGenericFixedArity8(b *testing.B) {
	s := store.New(N)
	run, locks := System8(genericSys8, s) // all 8 type args inferred
	if len(locks) != 8 {
		b.Fatal("expected 8 lock types")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

func TestGenericArityInfersLockSet(t *testing.T) {
	s := store.New(4)
	_, locks := System8(genericSys8, s)
	want := []reflect.Type{
		reflect.TypeFor[C1](), reflect.TypeFor[C2](), reflect.TypeFor[C3](), reflect.TypeFor[C4](),
		reflect.TypeFor[C5](), reflect.TypeFor[C6](), reflect.TypeFor[C7](), reflect.TypeFor[C8](),
	}
	for i := range want {
		if locks[i] != want[i] {
			t.Fatalf("lock %d: got %v want %v", i, locks[i], want[i])
		}
	}
	t.Logf("lock set derived from inferred type parameters: %v", locks)
}

// ---- registration-time instantiation, reached back through reflect.Type ----

// binder is NON-generic on purpose: an interface method may not have type
// parameters (see the findings file), so the generic work has to happen at a
// concrete call site and be stored behind a plain interface.
type binder interface {
	Bind(s *store.Store) reflect.Value
	Type() reflect.Type
}

type typedBinder[T any] struct{}

func (typedBinder[T]) Bind(s *store.Store) reflect.Value {
	return reflect.ValueOf(Query[T]{s: s})
}
func (typedBinder[T]) Type() reflect.Type { return reflect.TypeFor[Query[T]]() }

var registry = map[reflect.Type]binder{}

// RegisterComponent is the compile-time call site that creates the
// instantiation. After this, registry maps a runtime reflect.Type back to code
// the compiler already generated for T.
func RegisterComponent[T any]() {
	b := typedBinder[T]{}
	registry[b.Type()] = b
}

// bakeByReflection builds the per-frame call for a system of ARBITRARY arity by
// walking its reflect.Type and pulling each parameter's binder out of the
// registry. No generic instantiation happens here.
func bakeByReflection(fn any, s *store.Store) (func(), []reflect.Type, error) {
	v := reflect.ValueOf(fn)
	t := v.Type()
	args := make([]reflect.Value, t.NumIn())
	locks := make([]reflect.Type, t.NumIn())
	for i := range t.NumIn() {
		b, ok := registry[t.In(i)]
		if !ok {
			return nil, nil, fmt.Errorf("no binder registered for %v", t.In(i))
		}
		args[i] = b.Bind(s)
		locks[i] = t.In(i)
	}
	return func() { v.Call(args) }, locks, nil
}

func BenchmarkBakedByReflection8(b *testing.B) {
	RegisterComponent[C1]()
	RegisterComponent[C2]()
	RegisterComponent[C3]()
	RegisterComponent[C4]()
	RegisterComponent[C5]()
	RegisterComponent[C6]()
	RegisterComponent[C7]()
	RegisterComponent[C8]()
	s := store.New(N)
	run, locks, err := bakeByReflection(genericSys8, s)
	if err != nil {
		b.Fatal(err)
	}
	if len(locks) != 8 {
		b.Fatalf("want 8 locks, got %d", len(locks))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

func BenchmarkBakedByReflection3(b *testing.B) {
	RegisterComponent[store.Body]()
	RegisterComponent[Big]()
	RegisterComponent[int]()
	s := store.New(N)
	run, _, err := bakeByReflection(genericSys3, s)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

// ---- does reflect.Value.Call's sync.Pool frame survive parallelism and GC? ----
//
// reflect.Value.call takes the argument frame from a sync.Pool when the callee
// has no results (GOROOT/src/reflect/value.go:473-484, 594-598). sync.Pool is
// per-P, and its contents are dropped at every GC, so both GOMAXPROCS>1 and a
// GC-heavy process are places the zero-alloc result could quietly stop holding.

func BenchmarkReflectCall3Parallel(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sys3)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		args := []reflect.Value{reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s})}
		for pb.Next() {
			v.Call(args)
		}
	})
}

// churn keeps the GC running throughout the measured loop.
func startChurn(stop chan struct{}) {
	go func() {
		var keep [][]byte
		for {
			select {
			case <-stop:
				return
			default:
				keep = append(keep, make([]byte, 1<<16))
				if len(keep) > 256 {
					keep = keep[:0]
				}
			}
		}
	}()
}

func BenchmarkReflectCall3UnderGC(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sys3)
	args := []reflect.Value{reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s})}
	stop := make(chan struct{})
	startChurn(stop)
	defer close(stop)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Call(args)
	}
}

func BenchmarkReflectCall3ForcedGCEveryCall(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sys3)
	args := []reflect.Value{reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s})}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.GC()
		v.Call(args)
	}
}

// A callee whose arguments cannot fit in registers, so reflect must really
// allocate a stack frame -- combined with a GC before every call, which drops
// sync.Pool's primary store.
func sysBig4(a, b, c, d Big) { RSink += a.F[0] + b.F[0] + c.F[0] + d.F[0] }

func BenchmarkReflectCallBigFrame(b *testing.B) {
	v := reflect.ValueOf(sysBig4)
	args := []reflect.Value{
		reflect.ValueOf(Big{}), reflect.ValueOf(Big{}), reflect.ValueOf(Big{}), reflect.ValueOf(Big{})}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Call(args)
	}
}

func BenchmarkReflectCallBigFrameForcedGC(b *testing.B) {
	v := reflect.ValueOf(sysBig4)
	args := []reflect.Value{
		reflect.ValueOf(Big{}), reflect.ValueOf(Big{}), reflect.ValueOf(Big{}), reflect.ValueOf(Big{})}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.GC()
		runtime.GC()
		v.Call(args)
	}
}

// Control for BenchmarkReflectCallBigFrameForcedGC: the same two forced GCs
// with no reflective call at all.
func BenchmarkForcedGCOnly(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.GC()
		runtime.GC()
	}
}

// And the small-frame callee under the same two forced GCs.
func BenchmarkReflectCall3ForcedDoubleGC(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sys3)
	args := []reflect.Value{reflect.ValueOf(Q1{s}), reflect.ValueOf(Q2{s}), reflect.ValueOf(Q3{s})}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.GC()
		runtime.GC()
		v.Call(args)
	}
}

// 12 pointer args exceed amd64's 9 integer argument registers, so some spill to
// a stack frame. Under a dropped pool, does that frame get allocated?
func BenchmarkReflectCall12ForcedDoubleGC(b *testing.B) {
	s := store.New(N)
	v := reflect.ValueOf(sys12)
	args := make([]reflect.Value, 12)
	for i := range args {
		args[i] = reflect.ValueOf(s)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.GC()
		runtime.GC()
		v.Call(args)
	}
}
