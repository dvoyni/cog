package ecs

import (
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// The benchmarks here price the two halves of the binding: a value reaching a
// System through the signature instead of through the event, and a resource
// reaching one through the signature instead of through a Lock func. The bar
// for both is that nothing allocates, and the bar for In is that it costs
// nothing over naming the event — which is what makes "do not name the event"
// advice rather than a trade.

type namedEventSystem kernel.Subscription[app.UpdateEvent]

type fedSystem kernel.Subscription[app.UpdateEvent]

type unhoistedSystem kernel.Subscription[app.UpdateEvent]

type recordingSystem kernel.Subscription[app.UpdateEvent]

type publishingSystem kernel.Subscription[app.UpdateEvent]

// subscribeNamedEvent is the baseline: the System names app.UpdateEvent and can
// therefore only ever be subscribed to app.UpdateEvent.
func subscribeNamedEvent(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[namedEventSystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[moveQuery], tick app.UpdateEvent) {
			dt := float32(tick.Dt)
			for _, it := range q.All() {
				it.Body.X += it.Velocity.X * dt
				it.Body.Y += it.Velocity.Y * dt
			}
		}))
}

// subscribeFed is the same work reached through In, with Get hoisted out of the
// loop as the usage rule requires.
func subscribeFed(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[fedSystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[moveQuery], in *In[float32]) {
			dt := in.Get()
			for _, it := range q.All() {
				it.Body.X += it.Velocity.X * dt
				it.Body.Y += it.Velocity.Y * dt
			}
		},
		Feed(func(e app.UpdateEvent) float32 { return float32(e.Dt) })))
}

// subscribeUnhoisted is the mistake the usage rule exists to name: In is a
// pointer to a cell the adapter writes, so a Get inside the loop is a load the
// compiler cannot hoist past the Component writes — it cannot prove they do not
// alias.
func subscribeUnhoisted(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[unhoistedSystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[moveQuery], in *In[float32]) {
			for _, it := range q.All() {
				dt := in.Get()
				it.Body.X += it.Velocity.X * dt
				it.Body.Y += it.Velocity.Y * dt
			}
		},
		Feed(func(e app.UpdateEvent) float32 { return float32(e.Dt) })))
}

// subscribeRecording is the scene shape: Components read, a table read, a
// frame-local queue written, all named in the signature.
func subscribeRecording(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[recordingSystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[moveQuery], table *Read[*modelNames], out *Write[*drawLog]) {
			scale, queue := table.Get().Scale, out.Get()
			rows := queue.Xs[:0]
			for _, it := range q.All() {
				rows = append(rows, it.Body.X*scale)
			}
			queue.Xs = rows
		}))
}

// subscribePublishing is the one place on this ticket where an allocation can
// legitimately appear: a publication is a goroutine, a completion handle and an
// event context, and the kernel charges for all three.
func subscribePublishing(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[publishingSystem](ToHandler[app.UpdateEvent](world,
		func(handle kernel.Kernel, q *Query[moveQuery]) {
			count := 0
			for _, it := range q.All() {
				it.Body.X += it.Velocity.X
				count++
			}
			handle.PublishEvent(notice{Count: count})
		}))
	// A publication with nobody listening completes immediately and never
	// reaches the scheduler, so the number would flatter itself without this.
	registrar.Subscribe[noticeWatcher](ToHandler[notice](world, func(seen notice) {}))
}

// benchmarkBoundFrame is benchmarkFrame with the bound plugin composed beside
// the world, for the Systems that name its resources.
func benchmarkBoundFrame(b *testing.B, n int, subscribe func(*kernel.Registrar, *Entities)) {
	entities, components, engine := newWorldWith(b, uint32(n), subscribe, boundDeps,
		&bindingPlugin{log: &drawLog{Xs: make([]float32, 0, n)}, names: &modelNames{Scale: 2}})
	populate(entities, components, n)
	executioner := engine.Executioner()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
			b.Fatalf("publishing the update: %v", err)
		}
	}
}

func BenchmarkFrameNamedEvent1k(b *testing.B)  { benchmarkFrame(b, 1_000, subscribeNamedEvent) }
func BenchmarkFrameNamedEvent10k(b *testing.B) { benchmarkFrame(b, 10_000, subscribeNamedEvent) }
func BenchmarkFrameFed1k(b *testing.B)         { benchmarkFrame(b, 1_000, subscribeFed) }
func BenchmarkFrameFed10k(b *testing.B)        { benchmarkFrame(b, 10_000, subscribeFed) }
func BenchmarkFrameFedUnhoisted1k(b *testing.B) {
	benchmarkFrame(b, 1_000, subscribeUnhoisted)
}

func BenchmarkFrameFedUnhoisted10k(b *testing.B) {
	benchmarkFrame(b, 10_000, subscribeUnhoisted)
}

func BenchmarkFrameRecording1k(b *testing.B)  { benchmarkBoundFrame(b, 1_000, subscribeRecording) }
func BenchmarkFrameRecording10k(b *testing.B) { benchmarkBoundFrame(b, 10_000, subscribeRecording) }

// BenchmarkFramePublishing1k is the whole frame including the publication the
// System raises, and it is measured rather than asserted because it is the one
// number on this ticket that is not zero.
func BenchmarkFramePublishing1k(b *testing.B) { benchmarkFrame(b, 1_000, subscribePublishing) }

type hoistSystem kernel.Subscription[app.UpdateEvent]

// boundInput runs one real frame and hands back the Query and the In the kernel
// bound, so the walks below are the ones a System actually gets rather than
// values a test assembled. The whole-frame benchmarks cannot answer the hoist
// question: the engine's own per-publication charge and its scheduling variance
// are several microseconds, where the thing being measured is a load per Entity.
func boundInput(b *testing.B, n int) (*Query[moveQuery], *In[float32]) {
	b.Helper()
	var query *Query[moveQuery]
	var step *In[float32]
	entities, components, engine := newWorld(b, uint32(n), func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[hoistSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[moveQuery], dt *In[float32]) { query, step = q, dt },
			Feed(func(e app.UpdateEvent) float32 { return float32(e.Dt) })))
	})
	populate(entities, components, n)
	frame(b, engine, 1)
	return query, step
}

// BenchmarkStepHoisted is the walk with In.Get read once outside the loop, as
// the usage rule requires.
func BenchmarkStepHoisted(b *testing.B) {
	query, step := boundInput(b, 10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dt := step.Get()
		for _, it := range query.All() {
			it.Body.X += it.Velocity.X * dt
			it.Body.Y += it.Velocity.Y * dt
		}
	}
}

// BenchmarkStepUnhoisted is the same walk with the read inside the loop. In is a
// pointer to a cell the adapter writes, so the load cannot be hoisted past the
// Component writes: the compiler has no way to prove they do not alias.
func BenchmarkStepUnhoisted(b *testing.B) {
	query, step := boundInput(b, 10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, it := range query.All() {
			dt := step.Get()
			it.Body.X += it.Velocity.X * dt
			it.Body.Y += it.Velocity.Y * dt
		}
	}
}

// BenchmarkStepLocal is the floor: the same walk with the step in a local the
// compiler can keep in a register, which is what naming the event gives you —
// reflect hands the System a copy of the event struct, so nothing can alias it.
// Against BenchmarkStepHoisted it prices the In indirection itself.
func BenchmarkStepLocal(b *testing.B) {
	query, step := boundInput(b, 10_000)
	dt := step.Get()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, it := range query.All() {
			it.Body.X += it.Velocity.X * dt
			it.Body.Y += it.Velocity.Y * dt
		}
	}
}

// TestTheBoundFrameSitsOnTheEnginesAllocationLine is the allocation claim for
// everything this ticket adds, measured as a steady state at two entity counts
// an order of magnitude apart. The claims are three.
//
// In plus Feed costs no allocation over naming the event — identical counts,
// not merely small ones. A resource named in the signature costs none either;
// the handle is a window onto a cell, and a window is not a copy. And nothing
// scales with the entity count, which is what a per-Entity allocation would.
func TestTheBoundFrameSitsOnTheEnginesAllocationLine(t *testing.T) {
	const frames = 10_000
	measure := func(n int, bound bool, subscribe func(*kernel.Registrar, *Entities)) float64 {
		var entities *Entities
		var components *componentsPlugin
		var engine *kernel.Engine
		if bound {
			entities, components, engine = newWorldWith(t, uint32(n), subscribe, boundDeps,
				&bindingPlugin{log: &drawLog{Xs: make([]float32, 0, n)}, names: &modelNames{Scale: 2}})
		} else {
			entities, components, engine = newWorld(t, uint32(n), subscribe)
		}
		populate(entities, components, n)
		executioner := engine.Executioner()
		// Warm every pool the first frames fill, so what is measured is steady
		// state and not the first tick.
		for range 100 {
			frame(t, engine, 1)
		}
		mallocs := allocationsDuring(func() {
			for range frames {
				if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
					t.Fatalf("publishing the update: %v", err)
				}
			}
		})
		return float64(mallocs) / frames
	}

	named1k := measure(1_000, false, subscribeNamedEvent)
	named10k := measure(10_000, false, subscribeNamedEvent)
	fed1k := measure(1_000, false, subscribeFed)
	fed10k := measure(10_000, false, subscribeFed)
	recording1k := measure(1_000, true, subscribeRecording)
	recording10k := measure(10_000, true, subscribeRecording)
	t.Logf("objects a frame: named event at 1k %.3f, at 10k %.3f; In+Feed at 1k %.3f, at 10k %.3f; a resource in the signature at 1k %.3f, at 10k %.3f",
		named1k, named10k, fed1k, fed10k, recording1k, recording10k)

	if fed1k > named1k+0.05 || fed10k > named10k+0.05 {
		t.Fatalf("In+Feed costs %.3f/%.3f objects a frame against naming the event at %.3f/%.3f",
			fed1k, fed10k, named1k, named10k)
	}
	if fed10k > fed1k+0.05 {
		t.Fatalf("the projection allocates per Entity: %.3f a frame at 1k, %.3f at 10k", fed1k, fed10k)
	}
	if recording1k > named1k+0.05 || recording10k > recording1k+0.05 {
		t.Fatalf("a resource in the signature costs %.3f/%.3f objects a frame against %.3f",
			recording1k, recording10k, named1k)
	}
	if fed10k > 6.5 || recording10k > 6.5 {
		t.Fatalf("the frame costs %.3f/%.3f objects, above the engine's 6-per-frame line",
			fed10k, recording10k)
	}
}

// TestWhatPublishingFromASystemCosts measures the one thing on this ticket that
// does allocate, rather than leaving it to be discovered. A System may publish,
// and a publication is a goroutine with a completion handle and an event
// context; that is the kernel's price and not the ECS's, but a System is where
// a game will pay it, so the number belongs here.
func TestWhatPublishingFromASystemCosts(t *testing.T) {
	const frames = 2_000
	measure := func(subscribe func(*kernel.Registrar, *Entities)) float64 {
		entities, components, engine := newWorld(t, 1_000, subscribe)
		populate(entities, components, 1_000)
		executioner := engine.Executioner()
		for range 100 {
			frame(t, engine, 1)
		}
		mallocs := allocationsDuring(func() {
			for range frames {
				if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
					t.Fatalf("publishing the update: %v", err)
				}
			}
		})
		return float64(mallocs) / frames
	}
	quiet := measure(subscribeMove)
	publishing := measure(subscribePublishing)
	t.Logf("objects a frame: a silent System %.3f, a System publishing one event %.3f (%.3f for the publication)",
		quiet, publishing, publishing-quiet)
}

// benchmarkCommand prices one whole command invocation — dispatch, locks, the
// System, and the answer on its way back — for a System that answers and one
// that does not. The pair is what says whether the response wrapper costs
// anything, which is the question a response leaving through a cell raises.
func benchmarkCommand(b *testing.B, n int, system any) {
	entities, components, engine := newWorld(b, uint32(n), func(registrar *kernel.Registrar, world *Entities) {
		registrar.HandleCommand[nudgeCmd](ToExecute[nudgeRequest, nudgeResponse](world, system))
	})
	populate(entities, components, n)
	executioner := engine.Executioner()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := executioner.ExecuteCommand[nudgeCmd](nudgeRequest{By: 1}); err != nil {
			b.Fatalf("executing the System as a command: %v", err)
		}
	}
}

// sink keeps the silent arm's accumulation observable, so the two arms below do
// exactly the same per-Entity work and differ only in where the result goes.
var sink nudgeResponse

// BenchmarkCommandSilent1k is the instruction shape: the System computes the
// same thing and writes no answer, so the caller gets the zero response.
func BenchmarkCommandSilent1k(b *testing.B) {
	benchmarkCommand(b, 1_000, func(request nudgeRequest, q *Query[moveQuery]) {
		reply := nudgeResponse{}
		for _, it := range q.All() {
			it.Body.X += request.By
			reply.Moved++
			reply.Total += it.Body.X
		}
		sink = reply
	})
}

// BenchmarkCommandAnswering1k is the question shape: the same walk, the same
// locks, the same arithmetic, and the result written into the wrapper instead.
// Against the pair above, the difference is the response and nothing else.
func BenchmarkCommandAnswering1k(b *testing.B) {
	benchmarkCommand(b, 1_000, func(request nudgeRequest, q *Query[moveQuery], answer *Resp[nudgeResponse]) {
		reply := nudgeResponse{}
		for _, it := range q.All() {
			it.Body.X += request.By
			reply.Moved++
			reply.Total += it.Body.X
		}
		answer.Set(reply)
	})
}
