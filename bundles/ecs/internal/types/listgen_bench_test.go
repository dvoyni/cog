package types

// PROTOTYPE, throwaway: the List header at 24 bytes against 32, for
// https://github.com/dvoyni/cog/issues/380 (#268's generation in the header).

import (
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

type satchel struct {
	Items List[int32]
	X     float32
}

type satchelPlugin struct {
	ids   uint32
	store *Store[satchel]
}

func (p *satchelPlugin) Name() kernel.PluginName { return "satchel" }
func (p *satchelPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{Name}
}
func (p *satchelPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.store = RegisterComponent[satchel](registrar, p.ids)
	return nil
}

type (
	invReadQ struct {
		Inv  satchel
		Body *body
	}
	invWriteQ struct{ Inv *satchel }
	invSystem kernel.Subscription[app.UpdateEvent]
	invSet    struct{ Inv satchel }
)

func benchmarkInventoryFrame(b *testing.B, system any) {
	const n = 10_000
	plugin := &satchelPlugin{ids: n}
	entities, components, engine := newWorldWith(b, n, func(r *kernel.Registrar) {
		r.Subscribe[invSystem](ToHandler[app.UpdateEvent](r, system))
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
	benchmarkInventoryFrame(b, func(q *Query[invReadQ]) {
		for _, it := range q.All() {
			it.Body.X += it.Inv.X
		}
	})
}

func BenchmarkListHeaderWriteQuery10k(b *testing.B) {
	benchmarkInventoryFrame(b, func(q *Query[invWriteQ]) {
		for _, it := range q.All() {
			it.Inv.X++
		}
	})
}

func BenchmarkListHeaderStoreChurn(b *testing.B) {
	const n = 10_000
	en := newEntities(n)
	store := NewStore[satchel](en, n)
	ents := make([]Entity, n)
	value := satchel{Items: NewList[int32](1, 2, 3)}
	for i := range ents {
		ents[i] = en.alloc()
		store.Set(ents[i], value)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := ents[i%n]
		store.Remove(e)
		store.Set(e, value)
	}
}

func TestListHeaderSize(t *testing.T) {
	t.Logf("List header %d bytes, satchel row %d bytes", unsafe.Sizeof(List[int32]{}), unsafe.Sizeof(satchel{}))
}
