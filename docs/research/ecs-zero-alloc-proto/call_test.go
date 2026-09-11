package proto

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/app"

	"protoecs/ecs"
)

// The whole-frame benchmarks put the reflected builder about 1.4us a tick above
// the baked one, which is 16x what cog#234 measured for reflect.Value.Call at
// this arity. That gap is large enough to be a different effect rather than a
// different machine, so it is isolated here: the same two arguments, the same
// stable-cell trick, and a callee that does nothing at all.

type emptyQ struct {
	*Body
	Collider
}

var callSink int

func noopSystem(q *ecs.Query[emptyQ], ev app.UpdateEvent) { callSink++ }

func BenchmarkCallOnly_Reflect(b *testing.B) {
	q := &ecs.Query[emptyQ]{}
	evp := new(app.UpdateEvent)
	*evp = tick
	fnv := reflect.ValueOf(noopSystem)
	args := []reflect.Value{reflect.ValueOf(q), reflect.ValueOf(evp).Elem()}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		fnv.Call(args)
	}
}

func BenchmarkCallOnly_Direct(b *testing.B) {
	q := &ecs.Query[emptyQ]{}
	ev := tick
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		noopSystem(q, ev)
	}
}

// The pair above does not explain the whole-frame gap, so the next suspect is
// where the call happens rather than what it is. A publication runs its
// subscribers on a goroutine started for that publication, and a fresh
// goroutine starts with a small stack. If reflectcall needs more than that, the
// cost is a stack copy per tick -- paid by the reflected builder and not by the
// baked one, which is a plain call.
//
// Both benchmarks below start a goroutine per iteration, so the goroutine
// itself is common to the two and only the call differs.

func BenchmarkFreshGoroutine_Reflect(b *testing.B) {
	q := &ecs.Query[emptyQ]{}
	evp := new(app.UpdateEvent)
	*evp = tick
	fnv := reflect.ValueOf(noopSystem)
	args := []reflect.Value{reflect.ValueOf(q), reflect.ValueOf(evp).Elem()}
	done := make(chan struct{})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		go func() {
			fnv.Call(args)
			done <- struct{}{}
		}()
		<-done
	}
}

// The hybrid builder pinned the whole-frame gap on reflect.Value.Call itself,
// yet the same call in a tight loop costs 79ns. The remaining difference is how
// often it runs: once per System per frame, with a frame's worth of other code
// in between, against millions of times back to back.
//
// churn touches enough memory between calls to push reflect's call path out of
// cache, which is what a real frame does to it. Both benchmarks below pay the
// identical churn, so the difference is still only the call.

var churnBuf = make([]int64, 1<<16) // 512 KB, comfortably past L2

func churn() {
	for i := 0; i < len(churnBuf); i += 8 {
		churnBuf[i]++
	}
}

func BenchmarkCallOnly_ReflectCold(b *testing.B) {
	q := &ecs.Query[emptyQ]{}
	evp := new(app.UpdateEvent)
	*evp = tick
	fnv := reflect.ValueOf(noopSystem)
	args := []reflect.Value{reflect.ValueOf(q), reflect.ValueOf(evp).Elem()}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		churn()
		fnv.Call(args)
	}
}

func BenchmarkCallOnly_DirectCold(b *testing.B) {
	q := &ecs.Query[emptyQ]{}
	ev := tick
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		churn()
		noopSystem(q, ev)
	}
}

// Last suspect. The engine reaches a System down a chain of frames
// (runPublication, runTask, dispatch, the observe closure) on a goroutine
// started for that publication, and a fresh goroutine starts with a small
// stack. reflectcall needs a frame of its own on top of that, so if the stack
// has to grow, the cost is a whole stack copy -- paid every tick, because the
// grown stack dies with the goroutine.
//
// deep burns stack before calling, and both benchmarks burn the same amount.

//go:noinline
func deep(n int, fn func()) {
	if n == 0 {
		fn()
		return
	}
	var pad [512]byte
	sinkI += int(pad[0])
	deep(n-1, fn)
}

func BenchmarkDeepGoroutine_Reflect(b *testing.B) {
	q := &ecs.Query[emptyQ]{}
	evp := new(app.UpdateEvent)
	*evp = tick
	fnv := reflect.ValueOf(noopSystem)
	args := []reflect.Value{reflect.ValueOf(q), reflect.ValueOf(evp).Elem()}
	done := make(chan struct{})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		go func() {
			deep(8, func() { fnv.Call(args) })
			done <- struct{}{}
		}()
		<-done
	}
}

func BenchmarkDeepGoroutine_Direct(b *testing.B) {
	q := &ecs.Query[emptyQ]{}
	ev := tick
	done := make(chan struct{})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		go func() {
			deep(8, func() { noopSystem(q, ev) })
			done <- struct{}{}
		}()
		<-done
	}
}

func BenchmarkFreshGoroutine_Direct(b *testing.B) {
	q := &ecs.Query[emptyQ]{}
	ev := tick
	done := make(chan struct{})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		go func() {
			noopSystem(q, ev)
			done <- struct{}{}
		}()
		<-done
	}
}
