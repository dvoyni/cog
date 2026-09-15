package types

// The cost of the List header's generation, which every List user pays: the
// figures in ecs.md § The List come from these. A/B them by building this file
// into a test binary at a commit with the 24-byte header and one with the
// 32-byte header, and alternating the two.

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// satchel is one List and a float32: 28 bytes padded to 32 under a 24-byte
// header, and 36 padded to 40 under a 32-byte one, so the row crosses a size
// class.
type satchel struct {
	Items List[int32]
	X     float32
}

type satchelPlugin struct {
	ids   uint32
	store *Store[satchel]
}

func (p *satchelPlugin) Name() kernel.PluginName { return "satchel" }

func (p *satchelPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *satchelPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.store = RegisterComponent[satchel](registrar, p.ids)
	return nil
}

type (
	satchelReadQuery struct {
		Satchel satchel
		Body    *body
	}
	satchelWriteQuery struct {
		Satchel *satchel
	}
	satchelSystem kernel.Subscription[app.UpdateEvent]
)

func benchmarkSatchelFrame(b *testing.B, system any) {
	const n = 10_000
	plugin := &satchelPlugin{ids: n}
	entities, components, engine := newWorldWith(b, n, func(registrar *kernel.Registrar) {
		registrar.Subscribe[satchelSystem](ToHandler[app.UpdateEvent](registrar, system))
	}, []kernel.PluginName{Name, "components", "satchel"}, plugin)
	for i := range n {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		plugin.store.Set(e, satchel{Items: NewList[int32](1, 2, 3), X: float32(i)})
	}
	executioner := engine.Executioner()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListHeaderReadQuery10k(b *testing.B) {
	benchmarkSatchelFrame(b, func(q *Query[satchelReadQuery]) {
		for _, it := range q.All() {
			it.Body.X += it.Satchel.X
		}
	})
}

func BenchmarkListHeaderWriteQuery10k(b *testing.B) {
	benchmarkSatchelFrame(b, func(q *Query[satchelWriteQuery]) {
		for _, it := range q.All() {
			it.Satchel.X++
		}
	})
}

func BenchmarkListHeaderStoreRemoveAdd(b *testing.B) {
	const n = 10_000
	entities := newEntities(n)
	store := NewStore[satchel](entities, n)
	handles := make([]Entity, n)
	value := satchel{Items: NewList[int32](1, 2, 3)}
	for i := range handles {
		handles[i] = entities.alloc()
		store.Set(handles[i], value)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := handles[i%n]
		store.Remove(e)
		store.Set(e, value)
	}
}
