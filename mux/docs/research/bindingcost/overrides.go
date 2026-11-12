package bindingcost

import (
	"cmp"
	"slices"
	"unsafe"
)

// Easing is an enum, so a tween holds no func value.
type Easing uint8

const (
	EasingLinear Easing = iota
	EasingInOutQuad
)

func ease(easing Easing, t float32) float32 {
	if easing == EasingInOutQuad {
		if t < 0.5 {
			return 2 * t * t
		}
		return 1 - 2*(1-t)*(1-t)
	}
	return t
}

func progress(now, start, duration float64) (float32, bool) {
	if now < start || now >= start+duration {
		return 0, false
	}
	return float32((now - start) / duration), true
}

// ---- generic per-type collections ----

// Tween is one override of a T-typed property.
type Tween[T any] struct {
	Key      Key
	From, To T
	Start    float64
	Duration float64
	Easing   Easing
}

// Lerper is a value kind that interpolates itself.
type Lerper[T any] interface {
	Lerp(to T, t float32) T
}

// Lerp interpolates a Vec2.
func (from Vec2) Lerp(to Vec2, t float32) Vec2 {
	return Vec2{from.X + (to.X-from.X)*t, from.Y + (to.Y-from.Y)*t}
}

// Scalar is a float32 kind.
type Scalar float32

// Lerp interpolates a Scalar.
func (from Scalar) Lerp(to Scalar, t float32) Scalar { return from + (to-from)*Scalar(t) }

// Collection holds the tweens of one value type, interpolated through a type
// parameter constraint.
type Collection[T Lerper[T]] struct {
	Tweens []Tween[T]
}

// Evaluate writes the shown value of every tween active at now into out.
func (collection *Collection[T]) Evaluate(now float64, out []T) int {
	count := 0
	for i := range collection.Tweens {
		tween := &collection.Tweens[i]
		t, ok := progress(now, tween.Start, tween.Duration)
		if !ok {
			continue
		}
		out[count] = tween.From.Lerp(tween.To, ease(tween.Easing, t))
		count++
	}
	return count
}

// FuncCollection holds the tweens of one value type, interpolated through a func
// registered for the kind.
type FuncCollection[T any] struct {
	Tweens []Tween[T]
	Lerp   func(from, to T, t float32) T
}

// Evaluate writes the shown value of every tween active at now into out.
func (collection *FuncCollection[T]) Evaluate(now float64, out []T) int {
	count := 0
	for i := range collection.Tweens {
		tween := &collection.Tweens[i]
		t, ok := progress(now, tween.Start, tween.Duration)
		if !ok {
			continue
		}
		out[count] = collection.Lerp(tween.From, tween.To, ease(tween.Easing, t))
		count++
	}
	return count
}

// EvaluateVec2 is the hand-written, non-generic baseline.
func EvaluateVec2(tweens []Tween[Vec2], now float64, out []Vec2) int {
	count := 0
	for i := range tweens {
		tween := &tweens[i]
		t, ok := progress(now, tween.Start, tween.Duration)
		if !ok {
			continue
		}
		t = ease(tween.Easing, t)
		out[count] = Vec2{tween.From.X + (tween.To.X-tween.From.X)*t, tween.From.Y + (tween.To.Y-tween.From.Y)*t}
		count++
	}
	return count
}

// ---- type-erased fixed-size records ----

// Kind names the value kind of a record.
type Kind uint8

const (
	KindScalar Kind = iota
	KindVec2
)

// Record is one override of any kind, its values in fixed-size slots.
type Record struct {
	Key      Key
	From, To [4]float32
	Start    float64
	Duration float64
	Kind     Kind
	Easing   Easing
}

// EvaluateRecords writes the shown value of every active record into out, one
// [4]float32 slot per record, switching on the kind.
func EvaluateRecords(records []Record, now float64, out [][4]float32) int {
	count := 0
	for i := range records {
		record := &records[i]
		t, ok := progress(now, record.Start, record.Duration)
		if !ok {
			continue
		}
		t = ease(record.Easing, t)
		switch record.Kind {
		case KindScalar:
			out[count][0] = record.From[0] + (record.To[0]-record.From[0])*t
		case KindVec2:
			from := (*Vec2)(unsafe.Pointer(&record.From))
			to := (*Vec2)(unsafe.Pointer(&record.To))
			*(*Vec2)(unsafe.Pointer(&out[count])) = from.Lerp(*to, t)
		}
		count++
	}
	return count
}

// ---- boxed interface overrides, the naive shape, for reference ----

// Override is an interface every tween type implements.
type Override interface {
	Evaluate(now float64, out *[4]float32) bool
}

// BoxedTween is a tween behind the Override interface.
type BoxedTween[T Lerper[T]] struct{ Tween[T] }

// Evaluate writes the shown value into out when the tween is active.
func (tween *BoxedTween[T]) Evaluate(now float64, out *[4]float32) bool {
	t, ok := progress(now, tween.Start, tween.Duration)
	if !ok {
		return false
	}
	*(*T)(unsafe.Pointer(out)) = tween.From.Lerp(tween.To, ease(tween.Easing, t))
	return true
}

// ---- active-override lookup by key ----

// CompareKeys orders keys for the sorted slice.
func CompareKeys(a, b Key) int {
	if c := cmp.Compare(a.Path, b.Path); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Slot0, b.Slot0); c != 0 {
		return c
	}
	return cmp.Compare(a.Slot1, b.Slot1)
}

// SortedIndex is a sorted slice of keys, each with the index of its override.
type SortedIndex struct {
	Keys    []Key
	Indices []int32
}

// Find answers the index of key's override.
func (index *SortedIndex) Find(key Key) (int32, bool) {
	position, found := slices.BinarySearchFunc(index.Keys, key, CompareKeys)
	if !found {
		return 0, false
	}
	return index.Indices[position], true
}

// Insert keeps the slice sorted.
func (index *SortedIndex) Insert(key Key, value int32) {
	position, found := slices.BinarySearchFunc(index.Keys, key, CompareKeys)
	if found {
		index.Indices[position] = value
		return
	}
	index.Keys = slices.Insert(index.Keys, position, key)
	index.Indices = slices.Insert(index.Indices, position, value)
}

// Delete removes key.
func (index *SortedIndex) Delete(key Key) {
	position, found := slices.BinarySearchFunc(index.Keys, key, CompareKeys)
	if !found {
		return
	}
	index.Keys = slices.Delete(index.Keys, position, position+1)
	index.Indices = slices.Delete(index.Indices, position, position+1)
}
