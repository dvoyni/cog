package bindingcost

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"unsafe"
)

// Timing loops use the classic b.N form: b.Loop keeps loop-carried values alive
// through runtime.KeepAlive, which flattens differences in register use (see
// _legacy/docs/research/ecs-go-mechanics.md).

var counts = []int{16, 256, 4096}

const viewElementCount = 4096

const (
	pathUnitPosition uint32 = iota + 1
	pathCameraPosition
	pathPropPosition
)

var (
	sinkFloat float32
	sinkInt   int
)

func reportPerElement(b *testing.B, count int) {
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(count), "ns/elem")
}

// ---- reading a property's model value through each binding ----

func BenchmarkAccess(b *testing.B) {
	modelType := reflect.TypeFor[Model]()
	unitPath := CompilePath(modelType, "Units[].Position")
	cameraPath := CompilePath(modelType, "Camera.Position")
	propPathReflect := CompilePath(modelType, "Props{}.Position")
	propPathTyped := WithMapLookup[uint32, Prop](CompilePath(modelType, "Props{}.Position"))
	registry := []func(*Model, Key) *Vec2{pathUnitPosition: UnitPosition, pathCameraPosition: CameraPosition}

	for _, count := range counts {
		model := NewModel(count)
		base := unsafe.Pointer(model)
		var scratch Prop

		b.Run(fmt.Sprintf("slice/direct/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					sum += model.Units[i].Position.X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("slice/accessor/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					sum += UnitPosition(model, Key{Path: pathUnitPosition, Slot0: uint64(i)}).X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("slice/accessorRegistry/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					key := Key{Path: pathUnitPosition, Slot0: uint64(i)}
					sum += registry[key.Path](model, key).X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("slice/unsafePath/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					sum += (*Vec2)(unitPath.Resolve(base, Key{Path: pathUnitPosition, Slot0: uint64(i)}, nil)).X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("slice/reflectAtRead/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				units := reflect.ValueOf(model).Elem().FieldByName("Units")
				for i := range count {
					sum += float32(units.Index(i).FieldByName("Position").Field(0).Float())
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})

		b.Run(fmt.Sprintf("nested/direct/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for range count {
					sum += model.Camera.Position.X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("nested/accessorRegistry/%d", count), func(b *testing.B) {
			var sum float32
			key := Key{Path: pathCameraPosition}
			for range b.N {
				for range count {
					sum += registry[key.Path](model, key).X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("nested/unsafePath/%d", count), func(b *testing.B) {
			var sum float32
			key := Key{Path: pathCameraPosition}
			for range b.N {
				for range count {
					sum += (*Vec2)(cameraPath.Resolve(base, key, nil)).X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})

		b.Run(fmt.Sprintf("map/direct/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					sum += model.Props[uint32(i)].Position.X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("map/accessorCopy/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					position, _ := PropPosition(model, Key{Path: pathPropPosition, Slot0: uint64(i)})
					sum += position.X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("map/unsafePathTypedLookup/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					sum += (*Vec2)(propPathTyped.Resolve(base, Key{Path: pathPropPosition, Slot0: uint64(i)}, unsafe.Pointer(&scratch))).X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("map/unsafePathReflectLookup/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					sum += (*Vec2)(propPathReflect.Resolve(base, Key{Path: pathPropPosition, Slot0: uint64(i)}, unsafe.Pointer(&scratch))).X
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
	}
}

// ---- evaluating every active override of a frame ----

func makeTweens[T any](count int, value func(int) T) []Tween[T] {
	tweens := make([]Tween[T], count)
	for i := range tweens {
		tweens[i] = Tween[T]{
			Key: Key{Path: pathUnitPosition, Slot0: uint64(i)}, From: value(i), To: value(i + 1),
			Start: 0, Duration: 2, Easing: Easing(i % 2),
		}
	}
	return tweens
}

func vec2Value(i int) Vec2     { return Vec2{float32(i), float32(2 * i)} }
func scalarValue(i int) Scalar { return Scalar(i) }

func lerpVec2(from, to Vec2, t float32) Vec2       { return from.Lerp(to, t) }
func lerpScalar(from, to Scalar, t float32) Scalar { return from.Lerp(to, t) }

func vec2Record(tween Tween[Vec2]) Record {
	return Record{Key: tween.Key, From: [4]float32{tween.From.X, tween.From.Y}, To: [4]float32{tween.To.X, tween.To.Y},
		Start: tween.Start, Duration: tween.Duration, Kind: KindVec2, Easing: tween.Easing}
}

func scalarRecord(tween Tween[Scalar]) Record {
	return Record{Key: tween.Key, From: [4]float32{float32(tween.From)}, To: [4]float32{float32(tween.To)},
		Start: tween.Start, Duration: tween.Duration, Kind: KindScalar, Easing: tween.Easing}
}

func BenchmarkEvaluate(b *testing.B) {
	const now = 1.0
	for _, count := range counts {
		vec2Tweens := makeTweens(count, vec2Value)
		out := make([]Vec2, count)
		outRecords := make([][4]float32, count)

		// One value type: the generic shapes against the hand-written loop.
		vec2Records := make([]Record, count)
		for i, tween := range vec2Tweens {
			vec2Records[i] = vec2Record(tween)
		}
		constraint := &Collection[Vec2]{Tweens: vec2Tweens}
		withFunc := &FuncCollection[Vec2]{Tweens: vec2Tweens, Lerp: lerpVec2}
		b.Run(fmt.Sprintf("vec2/handwritten/%d", count), func(b *testing.B) {
			for range b.N {
				sinkInt += EvaluateVec2(vec2Tweens, now, out)
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("vec2/genericConstraint/%d", count), func(b *testing.B) {
			for range b.N {
				sinkInt += constraint.Evaluate(now, out)
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("vec2/genericFunc/%d", count), func(b *testing.B) {
			for range b.N {
				sinkInt += withFunc.Evaluate(now, out)
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("vec2/records/%d", count), func(b *testing.B) {
			for range b.N {
				sinkInt += EvaluateRecords(vec2Records, now, outRecords)
			}
			reportPerElement(b, count)
		})

		// Two value types, half each.
		half := count / 2
		mixedVec2 := &Collection[Vec2]{Tweens: makeTweens(half, vec2Value)}
		mixedScalar := &Collection[Scalar]{Tweens: makeTweens(half, scalarValue)}
		outScalar := make([]Scalar, half)
		grouped := make([]Record, 0, count)
		for _, tween := range mixedVec2.Tweens {
			grouped = append(grouped, vec2Record(tween))
		}
		for _, tween := range mixedScalar.Tweens {
			grouped = append(grouped, scalarRecord(tween))
		}
		random := rand.New(rand.NewPCG(1, 2))
		interleaved := slices.Clone(grouped)
		random.Shuffle(len(interleaved), func(i, j int) { interleaved[i], interleaved[j] = interleaved[j], interleaved[i] })
		boxed := make([]Override, 0, count)
		for i := range interleaved {
			record := interleaved[i]
			if record.Kind == KindVec2 {
				boxed = append(boxed, &BoxedTween[Vec2]{Tween[Vec2]{Key: record.Key, From: Vec2{record.From[0], record.From[1]},
					To: Vec2{record.To[0], record.To[1]}, Start: record.Start, Duration: record.Duration, Easing: record.Easing}})
			} else {
				boxed = append(boxed, &BoxedTween[Scalar]{Tween[Scalar]{Key: record.Key, From: Scalar(record.From[0]),
					To: Scalar(record.To[0]), Start: record.Start, Duration: record.Duration, Easing: record.Easing}})
			}
		}
		b.Run(fmt.Sprintf("mixed/genericCollections/%d", count), func(b *testing.B) {
			for range b.N {
				sinkInt += mixedVec2.Evaluate(now, out)
				sinkInt += mixedScalar.Evaluate(now, outScalar)
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("mixed/recordsGroupedByKind/%d", count), func(b *testing.B) {
			for range b.N {
				sinkInt += EvaluateRecords(grouped, now, outRecords)
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("mixed/recordsInterleaved/%d", count), func(b *testing.B) {
			for range b.N {
				sinkInt += EvaluateRecords(interleaved, now, outRecords)
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("mixed/boxedInterface/%d", count), func(b *testing.B) {
			for range b.N {
				for i, override := range boxed {
					if override.Evaluate(now, &outRecords[i]) {
						sinkInt++
					}
				}
			}
			reportPerElement(b, count)
		})
	}

	b.Run("schedule/genericCollectionAppend", func(b *testing.B) {
		collection := &Collection[Vec2]{Tweens: make([]Tween[Vec2], 0, 4096)}
		tween := makeTweens(1, vec2Value)[0]
		for range b.N {
			if len(collection.Tweens) == cap(collection.Tweens) {
				collection.Tweens = collection.Tweens[:0]
			}
			collection.Tweens = append(collection.Tweens, tween)
		}
	})
	b.Run("schedule/boxedInterfaceAppend", func(b *testing.B) {
		overrides := make([]Override, 0, 4096)
		tween := makeTweens(1, vec2Value)[0]
		for range b.N {
			if len(overrides) == cap(overrides) {
				overrides = overrides[:0]
			}
			overrides = append(overrides, &BoxedTween[Vec2]{tween})
		}
	})
}

// ---- finding the active override of a property ----

func makeKeys(count int, spread int, random *rand.Rand) []Key {
	slots := random.Perm(spread)[:count]
	keys := make([]Key, count)
	for i, slot := range slots {
		keys[i] = Key{Path: pathUnitPosition, Slot0: uint64(slot), Slot1: 0}
	}
	return keys
}

func BenchmarkLookup(b *testing.B) {
	for _, count := range counts {
		random := rand.New(rand.NewPCG(3, 4))
		keys := makeKeys(count, viewElementCount, random)
		byMap := make(map[Key]int32, count)
		sorted := &SortedIndex{}
		for i, key := range keys {
			byMap[key] = int32(i)
			sorted.Insert(key, int32(i))
		}

		// Every lookup hits: the active override of each of count properties, in
		// random order.
		b.Run(fmt.Sprintf("hits/map/%d", count), func(b *testing.B) {
			for range b.N {
				for _, key := range keys {
					index, ok := byMap[key]
					if ok {
						sinkInt += int(index)
					}
				}
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("hits/sorted/%d", count), func(b *testing.B) {
			for range b.N {
				for _, key := range keys {
					index, ok := sorted.Find(key)
					if ok {
						sinkInt += int(index)
					}
				}
			}
			reportPerElement(b, count)
		})

		// The same lookups with a key that has no padding (Key has 4 bytes of it
		// after Path), to see whether padding slows the hash. It does not.
		packedKeys := make([]PackedKey, count)
		byPackedMap := make(map[PackedKey]int32, count)
		for i, key := range keys {
			packedKeys[i] = PackedKey{Path: uint64(key.Path), Slot0: key.Slot0, Slot1: key.Slot1}
			byPackedMap[packedKeys[i]] = int32(i)
		}
		b.Run(fmt.Sprintf("hits/mapPackedKey/%d", count), func(b *testing.B) {
			for range b.N {
				for _, key := range packedKeys {
					index, ok := byPackedMap[key]
					if ok {
						sinkInt += int(index)
					}
				}
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("view/mapPackedKey/%d", count), func(b *testing.B) {
			for range b.N {
				for i := range viewElementCount {
					if index, ok := byPackedMap[PackedKey{Path: uint64(pathUnitPosition), Slot0: uint64(i)}]; ok {
						sinkInt += int(index)
					}
				}
			}
			reportPerElement(b, viewElementCount)
		})

		// A view over viewElementCount properties in model order, count of them
		// overridden: most lookups miss.
		b.Run(fmt.Sprintf("view/map/%d", count), func(b *testing.B) {
			for range b.N {
				for i := range viewElementCount {
					if index, ok := byMap[Key{Path: pathUnitPosition, Slot0: uint64(i)}]; ok {
						sinkInt += int(index)
					}
				}
			}
			reportPerElement(b, viewElementCount)
		})
		b.Run(fmt.Sprintf("view/sortedBinarySearch/%d", count), func(b *testing.B) {
			for range b.N {
				for i := range viewElementCount {
					if index, ok := sorted.Find(Key{Path: pathUnitPosition, Slot0: uint64(i)}); ok {
						sinkInt += int(index)
					}
				}
			}
			reportPerElement(b, viewElementCount)
		})
		b.Run(fmt.Sprintf("view/sortedMerge/%d", count), func(b *testing.B) {
			for range b.N {
				cursor := 0
				for i := range viewElementCount {
					key := Key{Path: pathUnitPosition, Slot0: uint64(i)}
					for cursor < len(sorted.Keys) && CompareKeys(sorted.Keys[cursor], key) < 0 {
						cursor++
					}
					if cursor < len(sorted.Keys) && sorted.Keys[cursor] == key {
						sinkInt += int(sorted.Indices[cursor])
					}
				}
			}
			reportPerElement(b, viewElementCount)
		})

		// Rebuilding the index from scratch, as after a load or a large update.
		b.Run(fmt.Sprintf("rebuild/map/%d", count), func(b *testing.B) {
			target := make(map[Key]int32, count)
			for range b.N {
				clear(target)
				for i, key := range keys {
					target[key] = int32(i)
				}
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("rebuild/sortedAppendThenSort/%d", count), func(b *testing.B) {
			target := &SortedIndex{Keys: make([]Key, 0, count), Indices: make([]int32, 0, count)}
			order := make([]int32, count)
			for range b.N {
				target.Keys = append(target.Keys[:0], keys...)
				slices.SortFunc(target.Keys, CompareKeys)
				for i := range order {
					order[i] = int32(i) // stands in for carrying indices along with the sort
				}
				target.Indices = append(target.Indices[:0], order...)
			}
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("rebuild/sortedInsertEach/%d", count), func(b *testing.B) {
			target := &SortedIndex{Keys: make([]Key, 0, count), Indices: make([]int32, 0, count)}
			for range b.N {
				target.Keys = target.Keys[:0]
				target.Indices = target.Indices[:0]
				for i, key := range keys {
					target.Insert(key, int32(i))
				}
			}
			reportPerElement(b, count)
		})

		// Steady state: one override ends and another starts, count active.
		b.Run(fmt.Sprintf("churn/map/%d", count), func(b *testing.B) {
			target := make(map[Key]int32, count)
			for i, key := range keys {
				target[key] = int32(i)
			}
			for n := range b.N {
				key := keys[n%count]
				delete(target, key)
				target[key] = int32(n)
			}
		})
		b.Run(fmt.Sprintf("churn/sorted/%d", count), func(b *testing.B) {
			target := &SortedIndex{Keys: slices.Clone(sorted.Keys), Indices: slices.Clone(sorted.Indices)}
			target.Keys = slices.Grow(target.Keys, 1)
			target.Indices = slices.Grow(target.Indices, 1)
			for n := range b.N {
				key := keys[n%count]
				target.Delete(key)
				target.Insert(key, int32(n))
			}
		})
	}
}

// ---- a whole view: the shown position of every unit ----

func BenchmarkShown(b *testing.B) {
	const now = 1.0
	modelType := reflect.TypeFor[Model]()
	unitPath := CompilePath(modelType, "Units[].Position")
	registry := []func(*Model, Key) *Vec2{pathUnitPosition: UnitPosition}
	for _, count := range counts {
		model := NewModel(count)
		base := unsafe.Pointer(model)
		random := rand.New(rand.NewPCG(5, 6))
		keys := makeKeys(count/4, count, random)
		tweens := make([]Tween[Vec2], len(keys))
		for i, key := range keys {
			tweens[i] = Tween[Vec2]{Key: key, From: Vec2{}, To: Vec2{1, 1}, Start: 0, Duration: 2}
		}
		byMap := make(map[Key]int32, len(keys))
		sorted := &SortedIndex{}
		for i, key := range keys {
			byMap[key] = int32(i)
			sorted.Insert(key, int32(i))
		}
		shown := func(index int32) Vec2 {
			tween := &tweens[index]
			t, _ := progress(now, tween.Start, tween.Duration)
			return tween.From.Lerp(tween.To, ease(tween.Easing, t))
		}

		b.Run(fmt.Sprintf("mapAndAccessorRegistry/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				for i := range count {
					key := Key{Path: pathUnitPosition, Slot0: uint64(i)}
					if index, ok := byMap[key]; ok {
						sum += shown(index).X
					} else {
						sum += registry[key.Path](model, key).X
					}
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("sortedMergeAndUnsafePath/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				cursor := 0
				for i := range count {
					key := Key{Path: pathUnitPosition, Slot0: uint64(i)}
					for cursor < len(sorted.Keys) && CompareKeys(sorted.Keys[cursor], key) < 0 {
						cursor++
					}
					if cursor < len(sorted.Keys) && sorted.Keys[cursor] == key {
						sum += shown(sorted.Indices[cursor]).X
					} else {
						sum += (*Vec2)(unitPath.Resolve(base, key, nil)).X
					}
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
		b.Run(fmt.Sprintf("sortedMergeAndDirect/%d", count), func(b *testing.B) {
			var sum float32
			for range b.N {
				cursor := 0
				for i := range count {
					key := Key{Path: pathUnitPosition, Slot0: uint64(i)}
					for cursor < len(sorted.Keys) && CompareKeys(sorted.Keys[cursor], key) < 0 {
						cursor++
					}
					if cursor < len(sorted.Keys) && sorted.Keys[cursor] == key {
						sum += shown(sorted.Indices[cursor]).X
					} else {
						sum += model.Units[i].Position.X
					}
				}
			}
			sinkFloat = sum
			reportPerElement(b, count)
		})
	}
}

// TestResolveAgreesWithDirect checks the unsafe path reads what the direct code
// reads, including after the slice is reallocated by append.
func TestResolveAgreesWithDirect(t *testing.T) {
	model := NewModel(4)
	modelType := reflect.TypeFor[Model]()
	unitPath := CompilePath(modelType, "Units[].Position")
	propPath := WithMapLookup[uint32, Prop](CompilePath(modelType, "Props{}.Position"))
	propReflect := CompilePath(modelType, "Props{}.Position")
	model.Units = append(model.Units, Unit{ID: 4, Position: Vec2{40, 41}}) // reallocates
	got := *(*Vec2)(unitPath.Resolve(unsafe.Pointer(model), Key{Slot0: 4}, nil))
	if got != (Vec2{40, 41}) {
		t.Fatalf("slice: got %v", got)
	}
	var scratch Prop
	if got := *(*Vec2)(propPath.Resolve(unsafe.Pointer(model), Key{Slot0: 2}, unsafe.Pointer(&scratch))); got != model.Props[2].Position {
		t.Fatalf("map typed: got %v", got)
	}
	if got := *(*Vec2)(propReflect.Resolve(unsafe.Pointer(model), Key{Slot0: 3}, unsafe.Pointer(&scratch))); got != model.Props[3].Position {
		t.Fatalf("map reflect: got %v", got)
	}
	if unitPath.Resolve(unsafe.Pointer(model), Key{Slot0: 99}, nil) != nil {
		t.Fatal("out of range index resolved")
	}
}

// TestZeroAllocations is the allocation bar, measured with AllocsPerRun.
func TestZeroAllocations(t *testing.T) {
	model := NewModel(256)
	modelType := reflect.TypeFor[Model]()
	unitPath := CompilePath(modelType, "Units[].Position")
	propTyped := WithMapLookup[uint32, Prop](CompilePath(modelType, "Props{}.Position"))
	propReflect := CompilePath(modelType, "Props{}.Position")
	base := unsafe.Pointer(model)
	var scratch Prop
	collection := &Collection[Vec2]{Tweens: makeTweens(256, vec2Value)}
	out := make([]Vec2, 256)
	keys := makeKeys(64, 256, rand.New(rand.NewPCG(7, 8)))
	byMap := map[Key]int32{}
	sorted := &SortedIndex{}
	for i, key := range keys {
		byMap[key] = int32(i)
		sorted.Insert(key, int32(i))
	}
	cases := map[string]func(){
		"unsafePathSlice":        func() { _ = unitPath.Resolve(base, Key{Slot0: 7}, nil) },
		"unsafePathTypedMap":     func() { _ = propTyped.Resolve(base, Key{Slot0: 7}, unsafe.Pointer(&scratch)) },
		"unsafePathReflectMap":   func() { _ = propReflect.Resolve(base, Key{Slot0: 7}, unsafe.Pointer(&scratch)) },
		"genericEvaluate":        func() { _ = collection.Evaluate(1, out) },
		"mapLookup":              func() { _ = byMap[keys[3]] },
		"sortedFind":             func() { _, _ = sorted.Find(keys[3]) },
		"mapChurn":               func() { delete(byMap, keys[3]); byMap[keys[3]] = 3 },
		"sortedChurn":            func() { sorted.Delete(keys[3]); sorted.Insert(keys[3], 3) },
		"boxedInterfaceSchedule": func() { sinkOverride = &BoxedTween[Vec2]{collection.Tweens[0]} },
	}
	for name, run := range cases {
		t.Logf("%-24s allocs/run = %v", name, testing.AllocsPerRun(1000, run))
	}
}

var sinkOverride Override

// PackedKey is Key with no padding.
type PackedKey struct {
	Path  uint64
	Slot0 uint64
	Slot1 uint64
}
