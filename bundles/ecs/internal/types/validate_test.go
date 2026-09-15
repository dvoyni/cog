//go:build ecs_validate

package types

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// listsPlugin owns the one Component these tests need. It is its own plugin
// rather than a field on componentsPlugin because every other test in this
// package counts Store lengths to reason about driver selection, and a
// Component nobody asked for would change what they measure.
type listsPlugin struct {
	ids        uint32
	inventorys *Store[inventory]
	grids      *Store[grid]
}

func (p *listsPlugin) Name() kernel.PluginName { return "lists" }

func (p *listsPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *listsPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.inventorys = RegisterComponent[inventory](registrar, p.ids)
	p.grids = RegisterComponent[grid](registrar, p.ids)
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
func listWorld(t *testing.T, subscribe func(*kernel.Registrar)) {
	t.Helper()
	lists := &listsPlugin{ids: 16}
	_, _, engine := newWorldWith(t, 16, subscribe,
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
	listWorld(t, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[readInventory], spawn *Spawn[inventorySet]) {
			spawn.New(inventorySet{Inventory: inventory{Slots: ListOf([]uint32{1, 2, 3})}})
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
	listWorld(t, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[writeInventory], spawn *Spawn[inventorySet]) {
			spawn.New(inventorySet{Inventory: inventory{Slots: ListOf([]uint32{1, 2, 3})}})
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
	listWorld(t, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[writeInventory], spawn *Spawn[inventorySet]) {
			spawn.New(inventorySet{Inventory: inventory{Slots: ListOf([]uint32{1, 2, 3})}})
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
	entities := newEntities(8)
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

// TestAWriteThroughANestedReadIsCaught is the read case one List further out:
// the List a read yielded holds elements that name backing arrays of their
// own, and a write into one of those is the same write into the Store. The
// stamp walks the outer List's elements, so it panics exactly as the flat case
// does.
func TestAWriteThroughANestedReadIsCaught(t *testing.T) {
	var caught string
	listWorld(t, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[readGrid], spawn *Spawn[gridSet]) {
			spawn.New(gridSet{Grid: twoByTwo()})
			for _, it := range q.All() {
				cells := it.Grid.Rows.At(1).Cells
				caught = recovered(func() { cells.Set(0, 99) })
			}
		}))
	})
	if caught == "" {
		t.Fatal("writing a nested List through a read field was allowed")
	}
	if !strings.Contains(caught, "read of ecs.grid") {
		t.Fatalf("the panic does not name the Component and the mode: %q", caught)
	}
}

// TestAWriteThroughANestedWriteIsAllowed is the other half for nested Lists:
// a walk that stamped the outer backing and left the inner ones at the stamp
// they entered the Store with would refuse the legal write.
func TestAWriteThroughANestedWriteIsAllowed(t *testing.T) {
	var caught string
	var seen uint32
	listWorld(t, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[writeGrid], spawn *Spawn[gridSet]) {
			spawn.New(gridSet{Grid: twoByTwo()})
			for _, it := range q.All() {
				cells := it.Grid.Rows.At(1).Cells
				caught = recovered(func() { cells.Set(0, 99) })
				seen = it.Grid.Rows.At(1).Cells.At(0)
			}
		}))
	})
	if caught != "" {
		t.Fatalf("writing a nested List through a write field was refused: %s", caught)
	}
	if seen != 99 {
		t.Fatalf("the write did not land: element 0 is %d, want 99", seen)
	}
}

// TestTheCallersOwnNestedCopyIsClosedByStoring is the constructor alias one
// level down: the inner List the caller built is shared with the Store the
// moment the outer one enters it.
func TestTheCallersOwnNestedCopyIsClosedByStoring(t *testing.T) {
	entities := newEntities(8)
	store := NewStore[grid](entities, 8)

	cells := ListOf([]uint32{1, 2})
	store.Set(entities.alloc(), grid{Rows: NewList(row{Cells: cells})})
	caught := recovered(func() { cells.Set(0, 9) })
	if !strings.Contains(caught, "already in the world") {
		t.Fatalf("writing the caller's own alias of a nested stored List: %q", caught)
	}
}

// TestASetThroughASetOfCopyIsCaughtAndNamesRef is the case Hooks changed. The
// copy Set[C].Of hands out shares the stored backing array, but not the row, so
// a Set through it bumps the copy's generation and never reaches the stored
// Component's bytes, where a Changed Hook would look. It used to be allowed; it
// panics now, and the message names the route that works.
func TestASetThroughASetOfCopyIsCaughtAndNamesRef(t *testing.T) {
	var caught string
	lists := &listsPlugin{ids: 16}
	var e Entity
	worldOfOne(t, lists, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(set *Set[inventory]) {
			copied, _ := set.Of(e)
			caught = recovered(func() { copied.Slots.Set(0, 99) })
		}))
	}, func(entities *Entities) {
		e = entities.alloc()
		lists.inventorys.Set(e, inventory{Slots: ListOf([]uint32{1, 2, 3})})
	})
	if caught == "" {
		t.Fatal("writing a List through a Set.Of copy was allowed")
	}
	if !strings.Contains(caught, "Set[ecs.inventory].Ref") {
		t.Fatalf("the panic does not name Ref as the fix: %q", caught)
	}
}

// TestASetThroughANestedSetOfCopyIsCaughtAndNamesRef is the same copy one List
// further in: the inner array is reached through the same handle as the row, so
// it takes the same stamp and the same panic.
func TestASetThroughANestedSetOfCopyIsCaughtAndNamesRef(t *testing.T) {
	var caught string
	lists := &listsPlugin{ids: 16}
	var e Entity
	worldOfOne(t, lists, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(set *Set[grid]) {
			copied, _ := set.Of(e)
			cells := copied.Rows.At(1).Cells
			caught = recovered(func() { cells.Set(0, 99) })
		}))
	}, func(entities *Entities) {
		e = entities.alloc()
		lists.grids.Set(e, twoByTwo())
	})
	if caught == "" {
		t.Fatal("writing a nested List through a Set.Of copy was allowed")
	}
	if !strings.Contains(caught, "Set[ecs.grid].Ref") {
		t.Fatalf("the panic does not name Ref as the fix: %q", caught)
	}
}

// TestASetThroughRefIsAllowed is the route the Set.Of panic names, and the
// test that keeps the panic from being a ban: the stored List, reached through
// Ref, is the Component itself, flat or nested.
func TestASetThroughRefIsAllowed(t *testing.T) {
	var flat, nested string
	var seenFlat, seenNested uint32
	lists := &listsPlugin{ids: 16}
	var e Entity
	worldOfOne(t, lists, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(inventories *Set[inventory], grids *Set[grid]) {
			stored, _ := inventories.Ref(e)
			flat = recovered(func() { stored.Slots.Set(0, 99) })
			seenFlat = stored.Slots.At(0)

			storedGrid, _ := grids.Ref(e)
			cells := storedGrid.Rows.At(1).Cells
			nested = recovered(func() { cells.Set(0, 98) })
			seenNested = storedGrid.Rows.At(1).Cells.At(0)
		}))
	}, func(entities *Entities) {
		e = entities.alloc()
		lists.inventorys.Set(e, inventory{Slots: ListOf([]uint32{1, 2, 3})})
		lists.grids.Set(e, twoByTwo())
	})
	if flat != "" || nested != "" {
		t.Fatalf("writing a List through Ref was refused: flat %q, nested %q", flat, nested)
	}
	if seenFlat != 99 || seenNested != 98 {
		t.Fatalf("the writes did not land: flat %d, nested %d", seenFlat, seenNested)
	}
}

// TestASetOnAFreshListBeforeUpdateForIsAllowed is the first row of the
// legality table: a List no Store has seen is the caller's own, and so is a
// Set on it, right up to the UpdateFor that hands it to the world.
func TestASetOnAFreshListBeforeUpdateForIsAllowed(t *testing.T) {
	var caught string
	var stored uint32
	lists := &listsPlugin{ids: 16}
	var e Entity
	worldOfOne(t, lists, func(registrar *kernel.Registrar) {
		registrar.Subscribe[listSystem](ToHandler[app.UpdateEvent](registrar, func(set *Set[inventory]) {
			fresh := inventory{Slots: ListOf([]uint32{1, 2, 3})}
			caught = recovered(func() { fresh.Slots.Set(0, 42) })
			set.UpdateFor(e, fresh)
			value, _ := set.Of(e)
			stored = value.Slots.At(0)
		}))
	}, func(entities *Entities) {
		e = entities.alloc()
	})
	if caught != "" {
		t.Fatalf("writing a fresh List before UpdateFor was refused: %s", caught)
	}
	if stored != 42 {
		t.Fatalf("the stored List holds %d, want 42", stored)
	}
}

// worldOfOne builds the lists world, lets seed populate its Stores, and runs
// one update.
func worldOfOne(t *testing.T, lists *listsPlugin, subscribe func(*kernel.Registrar), seed func(*Entities)) {
	t.Helper()
	entities, _, engine := newWorldWith(t, 16, subscribe,
		[]kernel.PluginName{Name, "components", "lists"}, lists)
	seed(entities)
	if err := engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
		t.Fatalf("publishing the update: %v", err)
	}
}

func twoByTwo() grid {
	return grid{Rows: NewList(
		row{Cells: NewList[uint32](1, 2)},
		row{Cells: NewList[uint32](3, 4)},
	)}
}

type readGrid struct {
	Grid grid
}

type writeGrid struct {
	Grid *grid
}

type gridSet struct {
	Grid grid
}

// inventorySet is the Component set the tests above spawn with.
type inventorySet struct {
	Inventory inventory
}
