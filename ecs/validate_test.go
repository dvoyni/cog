//go:build ecs_validate

package ecs

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// listsPlugin owns the one Component these tests need. It is its own plugin
// rather than a field on componentsPlugin because every other test in this
// package counts Store lengths to reason about driver selection, and a
// Component nobody asked for would change what they measure.
type listsPlugin struct {
	world      *Entities
	ids        uint32
	inventorys *Store[inventory]
}

func (p *listsPlugin) Name() kernel.PluginName { return "lists" }

func (p *listsPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *listsPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.inventorys = RegisterComponent[inventory](registrar, p.world, p.ids)
	return nil
}

type readInventory struct {
	Inventory inventory
}

type writeInventory struct {
	Inventory *inventory
}

type listSystem kernel.Subscription[app.UpdateEvent]

// listWorld runs one System over one Entity carrying an inventory, and hands
// back whatever that System's body decided to report.
func listWorld(t *testing.T, subscribe func(*kernel.Registrar, *Entities)) {
	t.Helper()
	entities := NewEntities(16)
	lists := &listsPlugin{world: entities, ids: 16}
	_, _, engine := newWorldFor(t, entities, 16, subscribe,
		[]kernel.PluginName{Name, "components", "lists"}, lists)
	if err := engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
		t.Fatalf("publishing the update: %v", err)
	}
}

// recovered runs body and reports the panic message it produced, or the empty
// string if it produced none.
func recovered(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(error); ok {
				message = err.Error()
				return
			}
			message, _ = r.(string)
		}
	}()
	body()
	return ""
}

// TestAWriteThroughAReadIsCaught is the case the whole mode exists for: a
// System holding read{inventory} writing the Store through the List a read
// yielded, which no lock anywhere names and which a release build cannot see.
func TestAWriteThroughAReadIsCaught(t *testing.T) {
	var caught string
	listWorld(t, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[readInventory], spawn *Spawn[inventoryBundle]) {
				spawn.New(inventoryBundle{Inventory: inventory{Slots: ListOf([]uint32{1, 2, 3})}})
				for _, it := range q.All() {
					caught = recovered(func() { it.Inventory.Slots.Set(0, 99) })
				}
			}))
	})
	if caught == "" {
		t.Fatal("writing a List through a read field was allowed")
	}
	if !strings.Contains(caught, "read of ecs.inventory") {
		t.Fatalf("the panic does not name the Component and the mode: %q", caught)
	}
}

// TestAWriteThroughAWriteIsAllowed is the other half, and it is the half that
// makes the check worth having: a mode that refused the legal write too would
// just be a ban on Lists with extra steps.
func TestAWriteThroughAWriteIsAllowed(t *testing.T) {
	var caught string
	var seen uint32
	listWorld(t, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[writeInventory], spawn *Spawn[inventoryBundle]) {
				spawn.New(inventoryBundle{Inventory: inventory{Slots: ListOf([]uint32{1, 2, 3})}})
				for _, it := range q.All() {
					caught = recovered(func() { it.Inventory.Slots.Set(0, 99) })
					seen = it.Inventory.Slots.At(0)
				}
			}))
	})
	if caught != "" {
		t.Fatalf("writing a List through a write field was refused: %s", caught)
	}
	if seen != 99 {
		t.Fatalf("the write did not land: element 0 is %d, want 99", seen)
	}
}

// TestAWriteAfterTheRunIsCaught is the case no before-and-after comparison can
// attribute, because the window it happens in belongs to some unrelated System.
// The stamp travels with the backing array rather than with the loop, so the
// check fires wherever the write eventually happens.
func TestAWriteAfterTheRunIsCaught(t *testing.T) {
	var caught string
	listWorld(t, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[writeInventory], spawn *Spawn[inventoryBundle]) {
				spawn.New(inventoryBundle{Inventory: inventory{Slots: ListOf([]uint32{1, 2, 3})}})
				var kept List[uint32]
				for _, it := range q.All() {
					kept = it.Inventory.Slots
				}
				caught = recovered(func() { kept.Set(0, 99) })
			}))
	})
	if caught == "" {
		t.Fatal("writing a List retained past the run that yielded it was allowed")
	}
	if !strings.Contains(caught, "after the run") {
		t.Fatalf("the panic does not say the run had finished: %q", caught)
	}
}

// TestTheCallersOwnCopyIsClosedTooIsWhyStoringStamps records the alias a
// constructor leaves behind. ListOf copies, so the List a caller builds is its
// own — until Set copies the header into the Store, at which point the two
// share one array and the caller's retained value is a write handle on the
// world.
func TestTheCallersOwnCopyIsClosedByStoring(t *testing.T) {
	entities := NewEntities(8)
	store := NewStore[inventory](entities, 8)

	mine := ListOf([]uint32{1, 2, 3})
	if caught := recovered(func() { mine.Set(0, 7) }); caught != "" {
		t.Fatalf("writing a List the world has never seen was refused: %s", caught)
	}
	store.Set(entities.alloc(), inventory{Slots: mine})
	caught := recovered(func() { mine.Set(0, 9) })
	if caught == "" {
		t.Fatal("writing the caller's own alias of a stored List was allowed")
	}
	if !strings.Contains(caught, "already in the world") {
		t.Fatalf("the panic does not say the value had entered a Store: %q", caught)
	}
}

// inventoryBundle is the Bundle the tests above spawn with.
type inventoryBundle struct {
	Inventory inventory
}
