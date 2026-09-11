package bench

import (
	"iter"

	"github.com/dvoyni/cog/docs/research/ecs-go-mechanics-bench/store"
)

// These four functions are the same loop reached four different ways. They
// exist so `go build -gcflags='-m -m'` prints a short, readable escape report
// for each shape instead of one buried in benchmark scaffolding.

// EscInlined: the seq comes from a direct call on a concrete *Store whose All
// is inlinable. The range statement should see the func literal.
func EscInlined(s *store.Store) float32 {
	var sum float32
	for _, b := range s.All() {
		sum += b.X
	}
	return sum
}

// EscNotInlinable: same call shape, but AllFat is over the inline budget.
func EscNotInlinable(s *store.Store) float32 {
	var sum float32
	for _, b := range s.AllFat() {
		sum += b.X
	}
	return sum
}

// EscSeqLocal: the seq is parked in a local variable first. The compiler can
// still see its single assignment, so the range call is devirtualized.
func EscSeqLocal(s *store.Store) float32 {
	seq := s.All()
	var sum float32
	for _, b := range seq {
		sum += b.X
	}
	return sum
}

// EscSeqLocalTwoAssignments: the same local, but with two possible producers.
// This is where "the compiler can see the literal" stops being true.
func EscSeqLocalTwoAssignments(s *store.Store, fat bool) float32 {
	seq := s.All()
	if fat {
		seq = s.AllFat()
	}
	var sum float32
	for _, b := range seq {
		sum += b.X
	}
	return sum
}

// EscOpaqueParam: the seq arrives as a plain func parameter.
func EscOpaqueParam(seq iter.Seq2[store.Entity, store.Body]) float32 {
	var sum float32
	for _, b := range seq {
		sum += b.X
	}
	return sum
}

// EscViaAny: the seq arrives boxed in an any, as kernel.resource.value holds it.
func EscViaAny(v any) float32 {
	seq := v.(iter.Seq2[store.Entity, store.Body])
	var sum float32
	for _, b := range seq {
		sum += b.X
	}
	return sum
}

// EscViaHandle: the exact kernel.Read[iter.Seq2[...]] shape.
func EscViaHandle(r Read[iter.Seq2[store.Entity, store.Body]]) float32 {
	var sum float32
	for _, b := range r.Get() {
		sum += b.X
	}
	return sum
}

// EscViaStoreHandle: kernel.Read[*store.Store], seq built inside the loop scope.
func EscViaStoreHandle(r Read[*store.Store]) float32 {
	var sum float32
	for _, b := range r.Get().All() {
		sum += b.X
	}
	return sum
}

// EscBreak: does breaking out change anything?
func EscBreak(s *store.Store) float32 {
	var sum float32
	for id, b := range s.All() {
		sum += b.X
		if id == 10 {
			break
		}
	}
	return sum
}

// EscBreakViaHandle: break out of the boxed shape.
func EscBreakViaHandle(r Read[iter.Seq2[store.Entity, store.Body]]) float32 {
	var sum float32
	for id, b := range r.Get() {
		sum += b.X
		if id == 10 {
			break
		}
	}
	return sum
}

// EscCaptureOuter: the loop body writes to a variable declared outside the
// range statement, in the inlinable shape.
var outerSink float32

func EscCaptureOuter(s *store.Store, acc *float32) {
	for _, b := range s.All() {
		*acc += b.X
		outerSink = *acc
	}
}

// EscNested: a range-over-func inside another one.
func EscNested(s *store.Store) float32 {
	var sum float32
	for _, a := range s.All() {
		for _, c := range s.All() {
			sum += a.X * c.Y
		}
	}
	return sum
}

// EscReturnEarly: `return` out of a range-over-func body, which the compiler
// implements with the #state machinery plus a deferred check.
func EscReturnEarly(s *store.Store) float32 {
	for _, b := range s.All() {
		if b.X > 3 {
			return b.X
		}
	}
	return 0
}

// EscValueReceiver: All declared on a non-pointer receiver, called on a stored
// value.
func EscValueReceiver(s store.ValStore) float32 {
	var sum float32
	for _, b := range s.All() {
		sum += b.X
	}
	return sum
}

// EscReturnEarlyViaHandle: `return` out of the boxed shape.
func EscReturnEarlyViaHandle(r Read[iter.Seq2[store.Entity, store.Body]]) float32 {
	for _, b := range r.Get() {
		if b.X > 3 {
			return b.X
		}
	}
	return 0
}

// EscNestedViaHandle: the inner range statement is re-entered per outer element.
func EscNestedViaHandle(r Read[iter.Seq2[store.Entity, store.Body]]) float32 {
	var sum float32
	for _, a := range r.Get() {
		for _, c := range r.Get() {
			sum += a.X * c.Y
		}
	}
	return sum
}
