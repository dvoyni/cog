package ecs

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// benchEntities is how many entities each benchmark works on per round.
const benchEntities = 10_000

// benchSum keeps the reading loops from being optimized away.
var benchSum float32

// benchWorld is a world of benchEntities entities, each holding a position
// and a velocity, with the handles the benchmarks use outside any job.
type benchWorld struct {
	world          World[game]
	positions      Store[game, position]
	velocities     Store[game, velocity]
	moving         Query2[game, *position, velocity]
	drifting       Query2[game, *position, Optional[velocity]]
	placed         Query1[game, *position]
	velocityLookup Lookup[game, velocity]
	deltas         DeltaQuery1[game, position]
	commands       CommandBuffer[game, aiCommands]
	sink           CommandSink[game, aiCommands]
	entities       []Entity
}

// spawnBench spawns a benchWorld whose every velocity-th entity has a
// velocity; observed makes its positions observed from the first reset.
func spawnBench(b *testing.B, velocityEvery int, observed bool) *benchWorld {
	o := &benchWorld{}
	spawn(b, func(c *kernel.Configurator) {
		RegisterComponent[game, position](c, "position")
		RegisterComponent[game, velocity](c, "velocity")
		RegisterCommandBuffer[game, aiCommands](c)
		o.world = declare[World[game]](c)
		o.positions = declare[Store[game, position]](c)
		o.velocities = declare[Store[game, velocity]](c)
		o.moving = declare[Query2[game, *position, velocity]](c)
		o.drifting = declare[Query2[game, *position, Optional[velocity]]](c)
		o.placed = declare[Query1[game, *position]](c)
		o.velocityLookup = declare[Lookup[game, velocity]](c)
		if observed {
			o.deltas = declare[DeltaQuery1[game, position]](c)
		}
		o.commands = declare[CommandBuffer[game, aiCommands]](c)
		o.sink = declare[CommandSink[game, aiCommands]](c)
	})
	for i := range benchEntities {
		e := o.world.Spawn()
		o.entities = append(o.entities, e)
		o.positions.Add(e, position{X: float32(i)})
		if i%velocityEvery == 0 {
			o.velocities.Add(e, velocity{X: 1, Y: 1})
		}
	}
	if observed {
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	}
	return o
}

// measure runs round once to grow the containers to their working size,
// then times it, reporting the time per entity of a round of benchEntities.
func measure(b *testing.B, round func()) {
	round()
	b.ReportAllocs()
	for b.Loop() {
		round()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/benchEntities, "ns/entity")
}

// heapBytes answers the bytes allocated on the heap and still reachable.
func heapBytes() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

// reportHeap reports the heap reachable since before, per entity of a
// round, which o keeps reachable.
func reportHeap(b *testing.B, before uint64, o *benchWorld) {
	b.ReportMetric(float64(heapBytes()-before)/benchEntities, "heap-B/entity")
	runtime.KeepAlive(o)
}

func BenchmarkSliceLoop(b *testing.B) {
	positions := make([]position, benchEntities)
	velocities := make([]velocity, benchEntities)
	for i := range velocities {
		velocities[i] = velocity{X: 1, Y: 1}
	}
	measure(b, func() {
		for i := range positions {
			positions[i].X += velocities[i].X
			positions[i].Y += velocities[i].Y
		}
	})
}

func BenchmarkQueryDense(b *testing.B) {
	o := spawnBench(b, 1, false)
	measure(b, func() {
		for _, row := range o.moving.All() {
			pos, vel := row.Get()
			pos.X += vel.X
			pos.Y += vel.Y
		}
	})
}

func BenchmarkQueryDenseObserved(b *testing.B) {
	o := spawnBench(b, 1, true)
	measure(b, func() {
		for _, row := range o.moving.All() {
			pos, vel := row.Get()
			pos.X += vel.X
			pos.Y += vel.Y
		}
	})
}

// BenchmarkQuerySparse drives by the velocity store, a tenth the size of
// the position store it probes; the time is still per entity in the world.
func BenchmarkQuerySparse(b *testing.B) {
	o := spawnBench(b, 10, false)
	measure(b, func() {
		for _, row := range o.moving.All() {
			pos, vel := row.Get()
			pos.X += vel.X
		}
	})
}

// BenchmarkQueryOptional walks every position with a velocity on every
// tenth entity, read through an optional term.
func BenchmarkQueryOptional(b *testing.B) {
	o := spawnBench(b, 10, false)
	measure(b, func() {
		for _, row := range o.drifting.All() {
			pos, velocityTerm := row.Get()
			if vel, ok := velocityTerm.Get(); ok {
				pos.X += vel.X
			}
		}
	})
}

// BenchmarkQueryLookup is BenchmarkQueryOptional with the velocity read
// through a lookup probed per row.
func BenchmarkQueryLookup(b *testing.B) {
	o := spawnBench(b, 10, false)
	measure(b, func() {
		for e, row := range o.placed.All() {
			pos := row.Get()
			if vel, ok := o.velocityLookup.Get(e); ok {
				pos.X += vel.X
			}
		}
	})
}

func BenchmarkSliceIndexWrite(b *testing.B) {
	positions := make([]position, benchEntities)
	indices := make([]int, benchEntities)
	for i := range indices {
		indices[i] = (i * 7919) % benchEntities
	}
	measure(b, func() {
		for _, i := range indices {
			positions[i].X++
		}
	})
}

// BenchmarkStoreWrite writes by entity, in an order unrelated to the rows.
func BenchmarkStoreWrite(b *testing.B) {
	o := spawnBench(b, 1, false)
	measure(b, func() {
		for i := range benchEntities {
			o.positions.Write(o.entities[(i*7919)%benchEntities]).X++
		}
	})
}

func BenchmarkLookupGet(b *testing.B) {
	o := spawnBench(b, 1, false)
	measure(b, func() {
		for i := range benchEntities {
			benchSum += o.positions.Get(o.entities[(i*7919)%benchEntities]).X
		}
	})
}

// BenchmarkDirtyListChanges is the hand-written way to find a tenth of the
// values changed: a dirty list and a shadow slice, compared and copied back.
func BenchmarkDirtyListChanges(b *testing.B) {
	values := make([]position, benchEntities)
	shadows := make([]position, benchEntities)
	dirty := make([]bool, benchEntities)
	dirtyIndices := make([]int, 0, benchEntities)
	measure(b, func() {
		for i := 0; i < benchEntities; i += 10 {
			values[i].X++
			if !dirty[i] {
				dirty[i] = true
				dirtyIndices = append(dirtyIndices, i)
			}
		}
		for _, i := range dirtyIndices {
			if values[i] != shadows[i] {
				benchSum += values[i].X - shadows[i].X
			}
			shadows[i] = values[i]
			dirty[i] = false
		}
		dirtyIndices = dirtyIndices[:0]
	})
}

// BenchmarkDeltaQuery writes a tenth of an observed store, reads its deltas
// and resets the window.
func BenchmarkDeltaQuery(b *testing.B) {
	o := spawnBench(b, 1, true)
	measure(b, func() {
		for i := 0; i < benchEntities; i += 10 {
			o.positions.Write(o.entities[i]).X++
		}
		for _, row := range o.deltas.All() {
			benchSum += row.Get().X - row.Previous().X
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	})
}

// BenchmarkDeltaQueryShare is BenchmarkDeltaQuery with a share of the store
// written per window, from none to all of it, spread across its rows. It
// also reports the heap the world holds once the containers have grown.
func BenchmarkDeltaQueryShare(b *testing.B) {
	for _, percent := range []int{0, 1, 5, 10, 25, 50, 100} {
		b.Run(fmt.Sprintf("percent=%d", percent), func(b *testing.B) {
			before := heapBytes()
			o := spawnBench(b, 1, true)
			written := benchEntities * percent / 100
			measure(b, func() {
				for i := range written {
					o.positions.Write(o.entities[i*benchEntities/written]).X++
				}
				for _, row := range o.deltas.All() {
					benchSum += row.Get().X - row.Previous().X
				}
				ObservationResetSystem(nil, kernel.NoArg{}, o.world)
			})
			reportHeap(b, before, o)
		})
	}
}

// BenchmarkDeltaQueryLoad loads as many entities again into an observed
// world in one window, as a level load does, reads their deltas and resets;
// then it despawns them, reads the removals and resets.
func BenchmarkDeltaQueryLoad(b *testing.B) {
	before := heapBytes()
	o := spawnBench(b, 1, true)
	loaded := make([]Entity, benchEntities)
	measure(b, func() {
		for i := range loaded {
			e := o.world.Spawn()
			loaded[i] = e
			o.positions.Add(e, position{X: float32(i)})
		}
		for _, row := range o.deltas.All() {
			benchSum += row.Get().X
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
		for _, e := range loaded {
			o.world.Despawn(e)
		}
		for _, row := range o.deltas.All() {
			benchSum += row.Previous().X
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	})
	reportHeap(b, before, o)
}

// BenchmarkObservationResetUnchanged resets an observed store nobody wrote.
func BenchmarkObservationResetUnchanged(b *testing.B) {
	o := spawnBench(b, 1, true)
	measure(b, func() {
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	})
}

// BenchmarkCommitSet records a Set for every entity and commits it.
func BenchmarkCommitSet(b *testing.B) {
	o := spawnBench(b, 1, false)
	measure(b, func() {
		for i, e := range o.entities {
			o.commands.Set(e, position{X: float32(i)})
		}
		o.sink.Commit()
	})
}

func BenchmarkCommitSetObserved(b *testing.B) {
	o := spawnBench(b, 1, true)
	measure(b, func() {
		for i, e := range o.entities {
			o.commands.Set(e, position{X: float32(i)})
		}
		o.sink.Commit()
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	})
}

// BenchmarkCommitSpawnDespawn spawns benchEntities entities with two
// components through the buffer, commits, then despawns them all and
// commits again.
func BenchmarkCommitSpawnDespawn(b *testing.B) {
	o := spawnBench(b, 1, false)
	spawned := make([]Entity, benchEntities)
	measure(b, func() {
		for i := range spawned {
			e := o.commands.Reserve()
			spawned[i] = e
			o.commands.Spawn(e)
			o.commands.Add(e, position{X: 1})
			o.commands.Add(e, velocity{X: 1})
		}
		o.sink.Commit()
		for _, e := range spawned {
			o.commands.Despawn(e)
		}
		o.sink.Commit()
	})
}

// BenchmarkSpawnDespawn is BenchmarkCommitSpawnDespawn done directly under
// the barrier.
func BenchmarkSpawnDespawn(b *testing.B) {
	o := spawnBench(b, 1, false)
	spawned := make([]Entity, benchEntities)
	measure(b, func() {
		for i := range spawned {
			e := o.world.Spawn()
			spawned[i] = e
			o.positions.Add(e, position{X: 1})
			o.velocities.Add(e, velocity{X: 1})
		}
		for _, e := range spawned {
			o.world.Despawn(e)
		}
	})
}

// BenchmarkRefsGet reads refs of strings by handle, in an order unrelated
// to their slots.
func BenchmarkRefsGet(b *testing.B) {
	o := spawnRefs(b)
	handles := make([]Ref[string], benchEntities)
	for i := range handles {
		handles[i] = o.refs.New("name")
	}
	measure(b, func() {
		for i := range benchEntities {
			benchSum += float32(len(*o.lookup.Get(handles[(i*7919)%benchEntities])))
		}
	})
}

// BenchmarkRefsSet sets every ref of strings by handle, observed, in an
// order unrelated to their slots, then resets.
func BenchmarkRefsSet(b *testing.B) {
	o := spawnRefs(b)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	handles := make([]Ref[string], benchEntities)
	for i := range handles {
		handles[i] = o.refs.New("name")
	}
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	measure(b, func() {
		for i := range benchEntities {
			o.refs.Set(handles[(i*7919)%benchEntities], "other")
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	})
}

// BenchmarkDeltaRefs sets a tenth of benchEntities refs of strings, walks
// their deltas and resets.
func BenchmarkDeltaRefs(b *testing.B) {
	o := spawnRefs(b)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	handles := make([]Ref[string], benchEntities)
	for i := range handles {
		handles[i] = o.refs.New("name")
	}
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	measure(b, func() {
		for i := 0; i < benchEntities; i += 10 {
			o.refs.Set(handles[i], "other")
		}
		for _, delta := range o.refDeltas.All() {
			benchSum += float32(len(*delta.Value))
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	})
}

// BenchmarkKeyedRefsPut puts benchEntities meshes whose keys all have refs,
// as spawning models of loaded meshes does.
func BenchmarkKeyedRefsPut(b *testing.B) {
	o := spawnRefs(b)
	meshes := make([]mesh, benchEntities)
	for i := range meshes {
		meshes[i] = mesh{Path: fmt.Sprint("mesh", i)}
		o.meshes.Put(meshes[i])
	}
	measure(b, func() {
		for i := range benchEntities {
			benchSum += float32(o.meshes.Put(meshes[(i*7919)%benchEntities]).index)
		}
	})
}

// BenchmarkCommandRefsNewRelease makes benchEntities refs of strings
// through a buffer, commits, then releases them all through it and commits
// again.
func BenchmarkCommandRefsNewRelease(b *testing.B) {
	o := spawnCommandRefs(b)
	handles := make([]Ref[string], benchEntities)
	measure(b, func() {
		for i := range handles {
			handles[i] = o.first.New("name")
		}
		o.sink.Commit()
		for _, h := range handles {
			o.first.Release(h)
		}
		o.sink.Commit()
	})
}
