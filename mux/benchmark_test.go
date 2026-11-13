package mux

import (
	"fmt"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// elementCounts are the sizes a benchmark of a growing store runs at: how
// many elements a view reads, or how many overrides Step samples.
var elementCounts = [...]int{16, 256, 4096}

// viewUnitCount is how many units the view case walks, a quarter of them
// overridden.
const viewUnitCount = 4096

// variantElementCount is the size the depth and overlap variants of a
// benchmark run at.
const variantElementCount = 4096

// positionStepNames names the steps the paths of benchmarkModel.positionPaths
// cross to a unit's position, in their order.
var positionStepNames = [...]string{"slice", "map-uint32", "map-interned"}

// shallowDepth is a depth of one hole, beside the tests' depth of four.
type shallowDepth [1]int64

// deepDepth is a depth of twelve holes, beside the tests' depth of four.
type deepDepth [12]int64

// handTween is a tween a game with no mux writes by hand: a unit's position
// moving from one value to another over a span.
type handTween struct {
	start  time.Duration
	length time.Duration
	from   vec2
	to     vec2
}

// interpolateVec2 eases two vec2s component by component.
func interpolateVec2(a, b vec2, t float32, ease func(a, b, t float32) float32) vec2 {
	return vec2{X: ease(a.X, b.X, t), Y: ease(a.Y, b.Y, t)}
}

// benchmarkModel is game registered at depth D, its units held in its slice
// and in both its maps, with paths to each unit's position there and handles
// on its model and timeline, as a job's.
type benchmarkModel[D comparable] struct {
	model              kernel.Write[*game]
	timeline           kernel.Write[Timeline[game, D]]
	unitPosition       Path[game, vec2]
	unitByIDPosition   Path[game, vec2]
	unitByNamePosition Path[game, vec2]
	// names holds each unit's name at the index the timeline interned it
	// at, which is the unit's index in the slice and its id in UnitsByID.
	names []unitName
}

// spawnBenchmarkModel spawns a kernel of mux's plugin and a benchmarkModel of
// unitCount units.
func spawnBenchmarkModel[D comparable](b *testing.B, unitCount int) *benchmarkModel[D] {
	spawned := &benchmarkModel[D]{}
	spawn(b, func(configurator *kernel.Configurator) {
		RegisterModel[game, D](configurator)
		RegisterInterpolator(configurator, interpolateVec2)
		spawned.unitPosition = MakePath[game, vec2](configurator, "Units[].Position")
		spawned.unitByIDPosition = MakePath[game, vec2](configurator, "UnitsByID[].Position")
		spawned.unitByNamePosition = MakePath[game, vec2](configurator, "UnitsByName[].Position")
		spawned.model = declare[kernel.Write[*game]](configurator)
		spawned.timeline = declare[kernel.Write[Timeline[game, D]]](configurator)
	})
	model := spawned.model.Get()
	model.Units = make([]unit, unitCount)
	model.UnitsByID = make(map[uint32]unit, unitCount)
	model.UnitsByName = make(map[unitName]unit, unitCount)
	timeline := ElevateTimeline(spawned.timeline)
	spawned.names = make([]unitName, 0, unitCount)
	for index := range unitCount {
		position := vec2{X: float32(index), Y: 1}
		name := unitName(fmt.Sprint("unit", index))
		model.Units[index].Position = position
		model.UnitsByID[uint32(index)] = unit{Position: position}
		model.UnitsByName[name] = unit{Position: position}
		timeline.Intern(name)
		spawned.names = append(spawned.names, name)
	}
	return spawned
}

// positionPaths answers the paths to a unit's position through the slice,
// UnitsByID and UnitsByName, as positionStepNames names them.
func (m *benchmarkModel[D]) positionPaths() [3]Path[game, vec2] {
	return [3]Path[game, vec2]{m.unitPosition, m.unitByIDPosition, m.unitByNamePosition}
}

// runStepBody runs Step's body with delta, outside any job, where
// timelineProbe.runStep runs Step as a job.
func (m *benchmarkModel[D]) runStepBody(delta time.Duration) {
	step(nil, delta, m.model, m.timeline)
}

// overrideEveryFourthUnit tweens the position of every fourth of unitCount
// units, in the slice and in both maps, over an hour, and steps into it, so a
// quarter of the units read through the sample.
func (m *benchmarkModel[D]) overrideEveryFourthUnit(unitCount int) {
	timeline := ElevateTimeline(m.timeline)
	for index := 0; index < unitCount; index += 4 {
		for _, path := range m.positionPaths() {
			timeline.Tween(path, 0, vec2{}, vec2{X: 1, Y: 1}, time.Hour, EasingLinear, int64(index))
		}
	}
	m.runStepBody(time.Minute)
}

// measure runs round twice, so the stores grow to their working size, and
// fails b if a third run allocates. It then times round, reporting its
// allocations and the time per element of a round over elementCount, the
// element named by elementName.
func measure(b *testing.B, elementCount int, elementName string, round func()) {
	// AllocsPerRun runs round once before the run it counts.
	round()
	if allocations := testing.AllocsPerRun(1, round); allocations != 0 {
		b.Fatalf("a round over %d elements allocated %v times once the stores had grown", elementCount, allocations)
	}
	b.ReportAllocs()
	for b.Loop() {
		round()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(elementCount), "ns/"+elementName)
}

// runAtEachCount runs benchmark as a sub-benchmark at each of elementCounts,
// each named for its count of what countName names.
func runAtEachCount(b *testing.B, countName string, benchmark func(b *testing.B, count int)) {
	for _, count := range elementCounts {
		b.Run(fmt.Sprintf("%s=%d", countName, count), func(b *testing.B) { benchmark(b, count) })
	}
}

// BenchmarkHandRead times reading each unit's position by hand, by its index
// in the slice, its id in UnitsByID and its name in UnitsByName: what Get
// and Current read through paths.
func BenchmarkHandRead(b *testing.B) {
	for _, stepCase := range []struct {
		name string
		// makeRound answers a round reading each of spawned's count units.
		makeRound func(spawned *benchmarkModel[depth], count int) func()
	}{
		{"slice", func(spawned *benchmarkModel[depth], count int) func() {
			model := spawned.model.Get()
			return func() {
				for index := range count {
					positionSink = model.Units[index].Position
				}
			}
		}},
		{"map-uint32", func(spawned *benchmarkModel[depth], count int) func() {
			model := spawned.model.Get()
			return func() {
				for index := range count {
					positionSink = model.UnitsByID[uint32(index)].Position
				}
			}
		}},
		{"map-interned", func(spawned *benchmarkModel[depth], count int) func() {
			model := spawned.model.Get()
			return func() {
				for _, name := range spawned.names {
					positionSink = model.UnitsByName[name].Position
				}
			}
		}},
	} {
		b.Run("step="+stepCase.name, func(b *testing.B) {
			runAtEachCount(b, "elements", func(b *testing.B, count int) {
				measure(b, count, "element", stepCase.makeRound(spawnBenchmarkModel[depth](b, count), count))
			})
		})
	}
}

// BenchmarkGet times Get per unit read through the slice and through each
// map.
func BenchmarkGet(b *testing.B) {
	for stepIndex, stepName := range positionStepNames {
		b.Run("step="+stepName, func(b *testing.B) {
			runAtEachCount(b, "elements", func(b *testing.B, count int) {
				spawned := spawnBenchmarkModel[depth](b, count)
				model := spawned.model.Get()
				path := spawned.positionPaths()[stepIndex]
				measure(b, count, "element", func() {
					for index := range count {
						positionSink, _ = path.Get(model, int64(index))
					}
				})
			})
		})
	}
}

// BenchmarkCurrent times Current per unit read through the slice, in its
// three cases: on a path with nothing sampled, at an address with nothing
// sampled on a path with a quarter of its units overridden, and at an
// overridden one.
func BenchmarkCurrent(b *testing.B) {
	for _, readCase := range []string{"none", "elsewhere", "active"} {
		b.Run("case="+readCase, func(b *testing.B) {
			for _, count := range elementCounts {
				b.Run(fmt.Sprintf("elements=%d/depth=4", count), func(b *testing.B) { benchmarkCurrent[depth](b, readCase, count) })
			}
			b.Run(fmt.Sprintf("elements=%d/depth=1", variantElementCount), func(b *testing.B) { benchmarkCurrent[shallowDepth](b, readCase, variantElementCount) })
			b.Run(fmt.Sprintf("elements=%d/depth=12", variantElementCount), func(b *testing.B) { benchmarkCurrent[deepDepth](b, readCase, variantElementCount) })
		})
	}
}

// benchmarkCurrent times Current per unit read, as BenchmarkCurrent
// describes, of count units at depth D.
func benchmarkCurrent[D comparable](b *testing.B, readCase string, count int) {
	spawned := spawnBenchmarkModel[D](b, count)
	if readCase != "none" {
		spawned.overrideEveryFourthUnit(count)
	}
	var holes []int64
	for index := range int64(count) {
		if isOverridden := index%4 == 0; readCase == "none" || isOverridden == (readCase == "active") {
			holes = append(holes, index)
		}
	}
	model := spawned.model.Get()
	view := spawned.timeline.Get()
	measure(b, len(holes), "read", func() {
		for _, hole := range holes {
			positionSink, _ = view.Current(spawned.unitPosition, model, hole)
		}
	})
}

// BenchmarkHandLoop times ranging over the units by hand, in the slice and
// in each map: what All walks through paths.
func BenchmarkHandLoop(b *testing.B) {
	spawned := spawnBenchmarkModel[depth](b, viewUnitCount)
	model := spawned.model.Get()
	b.Run("over=slice", func(b *testing.B) {
		measure(b, viewUnitCount, "element", func() {
			for index := range model.Units {
				positionSink = model.Units[index].Position
			}
		})
	})
	b.Run("over=map-uint32", func(b *testing.B) {
		measure(b, viewUnitCount, "element", func() {
			for _, element := range model.UnitsByID {
				positionSink = element.Position
			}
		})
	})
	b.Run("over=map-interned", func(b *testing.B) {
		measure(b, viewUnitCount, "element", func() {
			for _, element := range model.UnitsByName {
				positionSink = element.Position
			}
		})
	})
}

// BenchmarkAll times the view case, walking 4096 units with a quarter of them
// overridden, in the slice and in each map.
func BenchmarkAll(b *testing.B) {
	spawned := spawnBenchmarkModel[depth](b, viewUnitCount)
	spawned.overrideEveryFourthUnit(viewUnitCount)
	model := spawned.model.Get()
	view := spawned.timeline.Get()
	paths := spawned.positionPaths()
	for walkIndex, walkName := range positionStepNames {
		b.Run("over="+walkName, func(b *testing.B) {
			path := paths[walkIndex]
			measure(b, viewUnitCount, "element", func() {
				for _, position := range view.All(path, model) {
					positionSink = position
				}
			})
		})
	}
}

// BenchmarkHandKeyLookup times looking each unit's name up in a map from
// names to indices by hand: what Intern of a known key and Index look up.
func BenchmarkHandKeyLookup(b *testing.B) {
	spawned := spawnBenchmarkModel[depth](b, viewUnitCount)
	indices := make(map[unitName]int64, viewUnitCount)
	for index, name := range spawned.names {
		indices[name] = int64(index)
	}
	measure(b, viewUnitCount, "key", func() {
		for _, name := range spawned.names {
			indexSink, foundSink = indices[name]
		}
	})
}

// BenchmarkIntern times Intern of keys the timeline holds.
func BenchmarkIntern(b *testing.B) {
	spawned := spawnBenchmarkModel[depth](b, viewUnitCount)
	timeline := ElevateTimeline(spawned.timeline)
	measure(b, viewUnitCount, "key", func() {
		for _, name := range spawned.names {
			indexSink = timeline.Intern(name)
		}
	})
}

// BenchmarkIndex times Index of keys the timeline holds, and of keys it
// never interned.
func BenchmarkIndex(b *testing.B) {
	spawned := spawnBenchmarkModel[depth](b, viewUnitCount)
	view := spawned.timeline.Get()
	absentNames := make([]unitName, viewUnitCount)
	for index := range absentNames {
		absentNames[index] = unitName(fmt.Sprint("absent", index))
	}
	for _, keyCase := range []struct {
		name  string
		names []unitName
	}{{"hit", spawned.names}, {"miss", absentNames}} {
		b.Run("key="+keyCase.name, func(b *testing.B) {
			measure(b, viewUnitCount, "key", func() {
				for _, name := range keyCase.names {
					indexSink, foundSink = view.Index(name)
				}
			})
		})
	}
}

// BenchmarkHandTween times tweening each unit's position by hand, eased
// linearly, as a game with no mux does: what Step samples.
func BenchmarkHandTween(b *testing.B) {
	runAtEachCount(b, "tweens", func(b *testing.B, count int) {
		tweens := make([]handTween, count)
		for index := range tweens {
			tweens[index] = handTween{length: time.Hour, to: vec2{X: 1, Y: 1}}
		}
		positions := make([]vec2, count)
		now := time.Minute
		measure(b, count, "override", func() {
			now += time.Microsecond
			for index := range tweens {
				tween := &tweens[index]
				progress := float32(float64(now-tween.start) / float64(tween.length))
				positions[index] = vec2{X: tween.from.X + (tween.to.X-tween.from.X)*progress, Y: tween.from.Y + (tween.to.Y-tween.from.Y)*progress}
			}
		})
	})
}

// BenchmarkStep times Step per override sampled: tweens eased linearly on
// units' positions, every one active, and those at one address blended. Each
// count, depth and overlap varies on its own from 4096 overrides at depth 4,
// one per address.
func BenchmarkStep(b *testing.B) {
	for _, count := range elementCounts {
		b.Run(fmt.Sprintf("overrides=%d/depth=4/overlap=1", count), func(b *testing.B) { benchmarkStep[depth](b, count, 1) })
	}
	b.Run(fmt.Sprintf("overrides=%d/depth=1/overlap=1", variantElementCount), func(b *testing.B) { benchmarkStep[shallowDepth](b, variantElementCount, 1) })
	b.Run(fmt.Sprintf("overrides=%d/depth=12/overlap=1", variantElementCount), func(b *testing.B) { benchmarkStep[deepDepth](b, variantElementCount, 1) })
	for _, overlap := range []int{2, 4} {
		b.Run(fmt.Sprintf("overrides=%d/depth=4/overlap=%d", variantElementCount, overlap), func(b *testing.B) { benchmarkStep[depth](b, variantElementCount, overlap) })
	}
}

// benchmarkStep times Step, as BenchmarkStep describes, of overrideCount
// overrides at depth D, overlap of them at each address, started a second
// apart.
func benchmarkStep[D comparable](b *testing.B, overrideCount, overlap int) {
	spawned := spawnBenchmarkModel[D](b, overrideCount/overlap)
	timeline := ElevateTimeline(spawned.timeline)
	for address := range int64(overrideCount / overlap) {
		for layer := range overlap {
			timeline.Tween(spawned.unitPosition, -time.Duration(layer)*time.Second, vec2{}, vec2{X: 1, Y: 1}, time.Hour, EasingLinear, address)
		}
	}
	measure(b, overrideCount, "override", func() { spawned.runStepBody(time.Microsecond) })
}

// BenchmarkTween times Tween per unit, with its sample and its retirement:
// each round tweens every unit over two of its Steps, so a round's Step
// samples its own tweens and retires the round before's.
func BenchmarkTween(b *testing.B) {
	runAtEachCount(b, "overrides", func(b *testing.B, count int) {
		spawned := spawnBenchmarkModel[depth](b, count)
		timeline := ElevateTimeline(spawned.timeline)
		measure(b, count, "override", func() {
			for index := range int64(count) {
				timeline.Tween(spawned.unitPosition, timeline.Now(), vec2{}, vec2{X: 1, Y: 1}, 2*time.Millisecond, EasingLinear, index)
			}
			spawned.runStepBody(time.Millisecond)
		})
	})
}

// BenchmarkHold times Hold per unit, with its sample and its retirement, as
// BenchmarkTween does Tween.
func BenchmarkHold(b *testing.B) {
	runAtEachCount(b, "overrides", func(b *testing.B, count int) {
		spawned := spawnBenchmarkModel[depth](b, count)
		timeline := ElevateTimeline(spawned.timeline)
		measure(b, count, "override", func() {
			for index := range int64(count) {
				timeline.Hold(spawned.unitPosition, timeline.Now(), vec2{X: 1, Y: 1}, 2*time.Millisecond, index)
			}
			spawned.runStepBody(time.Millisecond)
		})
	})
}

// BenchmarkSequence times a sequence per override it places, with their
// samples and retirements: each round walks every unit, then holds it, over
// a millisecond each, and steps once.
func BenchmarkSequence(b *testing.B) {
	runAtEachCount(b, "units", func(b *testing.B, count int) {
		spawned := spawnBenchmarkModel[depth](b, count)
		timeline := ElevateTimeline(spawned.timeline)
		measure(b, 2*count, "override", func() {
			for index := range int64(count) {
				sequence := timeline.MakeSequence(timeline.Now())
				sequence.Tween(spawned.unitPosition, vec2{}, vec2{X: 1, Y: 1}, time.Millisecond, EasingLinear, index)
				sequence.Hold(spawned.unitPosition, vec2{X: 1, Y: 1}, time.Millisecond, index)
			}
			spawned.runStepBody(time.Millisecond)
		})
	})
}

// BenchmarkCancel times Cancel per address, with the Tween it cancels: each
// round tweens every unit, then cancels each, in index order.
func BenchmarkCancel(b *testing.B) {
	runAtEachCount(b, "overrides", func(b *testing.B, count int) {
		spawned := spawnBenchmarkModel[depth](b, count)
		timeline := ElevateTimeline(spawned.timeline)
		measure(b, count, "override", func() {
			for index := range int64(count) {
				timeline.Tween(spawned.unitPosition, timeline.Now(), vec2{}, vec2{X: 1, Y: 1}, time.Hour, EasingLinear, index)
			}
			for index := range int64(count) {
				timeline.Cancel(spawned.unitPosition, index)
			}
		})
	})
}

// BenchmarkCall times Call per callback, with its call: each round schedules
// its callbacks at Now, and steps once to call them.
func BenchmarkCall(b *testing.B) {
	runAtEachCount(b, "callbacks", func(b *testing.B, count int) {
		spawned := spawnBenchmarkModel[depth](b, 0)
		timeline := ElevateTimeline(spawned.timeline)
		measure(b, count, "callback", func() {
			for range count {
				timeline.Call(timeline.Now(), countCall)
			}
			spawned.runStepBody(0)
		})
	})
}

// BenchmarkCancelCall times CancelCall per callback, with the Call it
// cancels: each round schedules its callbacks an hour on, then cancels each,
// in the order scheduled.
func BenchmarkCancelCall(b *testing.B) {
	runAtEachCount(b, "callbacks", func(b *testing.B, count int) {
		spawned := spawnBenchmarkModel[depth](b, 0)
		timeline := ElevateTimeline(spawned.timeline)
		indices := make([]int, count)
		measure(b, count, "callback", func() {
			for position := range indices {
				indices[position] = timeline.Call(timeline.Now()+time.Hour, countCall)
			}
			for _, index := range indices {
				timeline.CancelCall(index)
			}
		})
	})
}
