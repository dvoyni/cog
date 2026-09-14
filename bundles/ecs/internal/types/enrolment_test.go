package types

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The id authority and the Stores enrolled with it. The handle's own guarantees
// are tested in entities_test.go and entity_test.go; what is tested here is what
// the authority owes a Store.

// A despawn is total: it empties every Store, naming no Component type at all.
// Nothing records which Stores hold an entity, so every Store is asked — which
// is exactly why holding Entities for write is the one lock that covers a
// structural change, and why no per-entity index of "which Stores hold me" may
// ever be added.
func TestDespawnEmptiesEveryStore(t *testing.T) {
	entities := newEntities(8)
	positions := NewStore[position](entities, 8)
	tags := NewStore[disabled](entities, 8)
	doomed, bystander := entities.alloc(), entities.alloc()
	positions.Set(doomed, position{X: 1})
	tags.Set(doomed, disabled{})
	positions.Set(bystander, position{X: 2})

	if !entities.despawn(doomed) {
		t.Fatalf("despawn(%v) = false for a live entity", doomed)
	}
	if positions.Has(doomed) {
		t.Fatalf("the despawned entity still has a position")
	}
	if tags.Has(doomed) {
		t.Fatalf("the despawned entity still carries its tag")
	}
	if positions.Len() != 1 || !positions.Has(bystander) {
		t.Fatalf("the bystander lost its position: Len() = %d", positions.Len())
	}
	if value, ok := positions.Get(bystander); !ok || value.X != 2 {
		t.Fatalf("the bystander's value reads back as %v, %v; want {2 0}, true", value, ok)
	}
}

// Reclamation is eager and is therefore nothing at all: the row goes when the
// entity goes. This is the orphan-slot case, and it is measured rather than
// argued, because without eager removal each recycle of one index leaks one
// dense entry — a thousand of them here.
func TestRecyclingAnIndexLeaksNoDenseEntry(t *testing.T) {
	const cycles = 1000
	entities := newEntities(1)
	store := NewStore[position](entities, 1)

	peak := 0
	for i := range cycles {
		e := entities.alloc()
		store.Set(e, position{X: float32(i)})
		if store.Len() > peak {
			peak = store.Len()
		}
		if !entities.despawn(e) {
			t.Fatalf("despawn(%v) = false on cycle %d", e, i)
		}
	}

	if store.Len() != 0 {
		t.Fatalf("Len() = %d after %d recycles of one index, want 0", store.Len(), cycles)
	}
	if peak != 1 {
		t.Fatalf("the store held %d rows at its peak, want 1: one entity was ever alive", peak)
	}
	if cap(store.dense) != 1 {
		t.Fatalf("the dense array grew to %d rows over %d recycles, want 1", cap(store.dense), cycles)
	}
	if len(store.sparse) != 1 {
		t.Fatalf("the sparse index grew to %d slots, want 1: the free list bounds it", len(store.sparse))
	}
}

// Entities is the id authority, and the word World is retired from this API: a
// second simulation is a second Engine, never a second world, and CONTEXT.md
// lists World under Avoid. The rule is mechanical, so the check is too.
func TestNoIdentifierInTheBundleIsAWorld(t *testing.T) {
	var sources []string
	for _, pattern := range []string{"../../*.go", "../../ecsplugin/*.go", "../*.go", "*.go"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("listing the Bundle's sources: %v", err)
		}
		sources = append(sources, matches...)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", source, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && strings.Contains(strings.ToLower(identifier.Name), "world") {
				t.Errorf("%s names %q: the id authority is Entities and nothing in ecs is a World",
					source, identifier.Name)
			}
			return true
		})
	}
}
