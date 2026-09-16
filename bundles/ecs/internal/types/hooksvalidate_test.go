//go:build ecs_validate

package types

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// The Validation checks of hooks.md that only a validating build makes: IsX on
// a kind a kind set can never deliver, and List.Set through a Hook's value or
// on an array a second Component holds. The pace check, which a release build
// is also tested not to make, is in hookspace_test.go.

// askings is, for one kind set, which IsX methods panicked and which did not,
// over every record the reader was given.
type askings struct {
	records  int
	panicked map[string]bool
}

// askEvery asks every delivered record all five IsX methods.
func askEvery[K KindSet](h *Hooks[collider, K], into *askings) {
	for _, hook := range h.All() {
		into.records++
		for name, is := range map[string]func() bool{
			"IsSpawned": hook.IsSpawned, "IsDespawned": hook.IsDespawned,
			"IsAdded": hook.IsAdded, "IsRemoved": hook.IsRemoved, "IsChanged": hook.IsChanged,
		} {
			message := recovered(func() { is() })
			if message != "" && !strings.Contains(message, name) {
				panic(fmt.Sprintf("%s panicked without naming itself: %s", name, message))
			}
			into.panicked[name] = into.panicked[name] || message != ""
		}
	}
}

// TestIsXOnAKindTheSetCanNeverDeliverPanics is hooks.md § What IsX reports:
// under each kind set, IsX panics for exactly the kinds the table says the set
// can never deliver, and not for any other.
func TestIsXOnAKindTheSetCanNeverDeliverPanics(t *testing.T) {
	var asked [8]askings
	for i := range asked {
		asked[i].panicked = map[string]bool{}
	}
	w := newHookWorld(t, func(
		spawned *Hooks[collider, HookSpawned],
		despawned *Hooks[collider, HookDespawned],
		spawnedDespawned *Hooks[collider, HookSpawnedDespawned],
		added *Hooks[collider, HookAdded],
		removed *Hooks[collider, HookRemoved],
		addedRemoved *Hooks[collider, HookAddedRemoved],
		addedChanged *Hooks[collider, HookAddedChanged],
		all *Hooks[collider, HookAll],
	) {
		askEvery(spawned, &asked[0])
		askEvery(despawned, &asked[1])
		askEvery(spawnedDespawned, &asked[2])
		askEvery(added, &asked[3])
		askEvery(removed, &asked[4])
		askEvery(addedRemoved, &asked[5])
		askEvery(addedChanged, &asked[6])
		askEvery(all, &asked[7])
	})
	var spawned Entity
	w.structural(t, func(r restacking) {
		spawned = r.armed.New(armedSet{Collider: collider{Radius: 1}})
	})
	added := w.entities.alloc()
	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		set.UpdateFor(added, collider{Radius: 2})
	})
	w.walked(t, func(q *Query[colliderQuery], set *Set[collider], remove *Remove[collider]) {
		ref, _ := set.Ref(added)
		ref.Radius = 3
	})
	w.write(t, func(set *Set[collider], remove *Remove[collider]) { remove.From(added) })
	w.structural(t, func(r restacking) { r.we.Despawn(spawned) })
	w.read(t)

	impossible := map[string][]string{
		"HookSpawned":          {"IsRemoved", "IsDespawned"},
		"HookAdded":            {"IsRemoved", "IsDespawned"},
		"HookAddedChanged":     {"IsRemoved", "IsDespawned"},
		"HookDespawned":        {"IsAdded", "IsSpawned", "IsChanged"},
		"HookRemoved":          {"IsAdded", "IsSpawned", "IsChanged"},
		"HookSpawnedDespawned": nil,
		"HookAddedRemoved":     nil,
		"HookAll":              nil,
	}
	sets := []reflect.Type{
		reflect.TypeFor[HookSpawned](), reflect.TypeFor[HookDespawned](), reflect.TypeFor[HookSpawnedDespawned](),
		reflect.TypeFor[HookAdded](), reflect.TypeFor[HookRemoved](), reflect.TypeFor[HookAddedRemoved](),
		reflect.TypeFor[HookAddedChanged](), reflect.TypeFor[HookAll](),
	}
	for i, set := range sets {
		name := set.Name()
		if asked[i].records == 0 {
			t.Fatalf("%s was given no record to ask", name)
		}
		for _, is := range []string{"IsSpawned", "IsDespawned", "IsAdded", "IsRemoved", "IsChanged"} {
			want := strings.Contains(fmt.Sprint(impossible[name]), is)
			if asked[i].panicked[is] != want {
				t.Errorf("under %s, %s panicked=%v, want %v", name, is, asked[i].panicked[is], want)
			}
		}
	}
}

// pouch is a Component holding a List beside a plain field, and shelf one
// whose List's elements hold Lists: the flat and the nested case. Neither has
// implicit padding, as a Component watched for Changed must not.
type (
	pouch struct {
		Items List[uint32]
		Count uint32
		_     [4]byte
	}
	shelf    struct{ Rows List[shelfRow] }
	shelfRow struct{ Items List[uint32] }
)

// listWrite is what the scripted writer holds: Set and Remove on both
// Components, and the despawning handle.
type listWrite struct {
	pouches  *Set[pouch]
	unpack   *Remove[pouch]
	shelves  *Set[shelf]
	unshelve *Remove[shelf]
	we       *WriteableEntities
}

type (
	hookListWriteCmd kernel.Command[hookRequest, hookResponse]
	hookListReadCmd  kernel.Command[hookRequest, hookResponse]
)

// hookListsPlugin owns pouch and shelf, and registers the Systems a test
// hands it in its own Register.
type hookListsPlugin struct {
	pouches *Store[pouch]
	shelves *Store[shelf]
	own     func(*kernel.Registrar)
}

func (p *hookListsPlugin) Name() kernel.PluginName { return "hooklists" }

func (p *hookListsPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *hookListsPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.pouches = RegisterComponent[pouch](registrar, 16)
	p.shelves = RegisterComponent[shelf](registrar, 16)
	p.own(registrar)
	return nil
}

// hookListWorld is a world with a scripted writer of pouch and shelf, and a
// scripted reader of both under HookAll, each a command.
type hookListWorld struct {
	entities *Entities
	lists    *hookListsPlugin
	engine   *kernel.Engine
	write    func(lw listWrite)
	read     func(pouches *Hooks[pouch, HookAll], shelves *Hooks[shelf, HookAll])
}

// hookListReader is the reader System, a named method so a panic can be
// checked for naming it.
func (w *hookListWorld) hookListReader(pouches *Hooks[pouch, HookAll], shelves *Hooks[shelf, HookAll]) {
	w.read(pouches, shelves)
}

func newHookListWorld(t *testing.T) *hookListWorld {
	t.Helper()
	w := &hookListWorld{}
	w.lists = &hookListsPlugin{own: func(registrar *kernel.Registrar) {
		registrar.HandleCommand[hookListWriteCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(pouches *Set[pouch], unpack *Remove[pouch], shelves *Set[shelf], unshelve *Remove[shelf], we *WriteableEntities) {
				w.write(listWrite{pouches, unpack, shelves, unshelve, we})
			}))
		registrar.HandleCommand[hookListReadCmd](ToExecute[hookRequest, hookResponse](registrar, w.hookListReader))
	}}
	w.entities, _, w.engine = newWorldWith(t, 16, func(*kernel.Registrar) {}, nil, w.lists)
	return w
}

func (w *hookListWorld) writer(t *testing.T, write func(lw listWrite)) {
	t.Helper()
	w.write = write
	if _, err := w.engine.Executioner().ExecuteCommand[hookListWriteCmd](hookRequest{}); err != nil {
		t.Fatalf("running the writer: %v", err)
	}
}

func (w *hookListWorld) reader(t *testing.T, read func(pouches *Hooks[pouch, HookAll], shelves *Hooks[shelf, HookAll])) {
	t.Helper()
	w.read = read
	if _, err := w.engine.Executioner().ExecuteCommand[hookListReadCmd](hookRequest{}); err != nil {
		t.Fatalf("running the reader: %v", err)
	}
}

// hookKindOf renders the one kind a test tells records apart by.
func hookKindOf[T any](hook *Hook[T]) string {
	switch {
	case hook.IsRemoved():
		return "removal"
	case hook.IsAdded():
		return "addition"
	default:
		return "change"
	}
}

// TestASetThroughAHookValueIsCaught is hooks.md § List.Set through hook.Value
// is illegal, for every record: a Set on a List in a Hook's Value panics for an
// addition, a change and a removal, flat and nested, naming Hooks[T, K], the
// System and the fix, and again through a value kept into a later run.
func TestASetThroughAHookValueIsCaught(t *testing.T) {
	w := newHookListWorld(t)
	changed, removed := w.entities.alloc(), w.entities.alloc()
	for _, e := range []Entity{changed, removed} {
		w.lists.pouches.Set(e, pouch{Items: NewList[uint32](1, 2)})
		w.lists.shelves.Set(e, shelf{Rows: NewList(shelfRow{Items: NewList[uint32](1, 2)})})
	}
	added := w.entities.alloc()
	w.writer(t, func(lw listWrite) {
		lw.pouches.UpdateFor(added, pouch{Items: NewList[uint32](3, 4)})
		lw.shelves.UpdateFor(added, shelf{Rows: NewList(shelfRow{Items: NewList[uint32](3, 4)})})
		ref, _ := lw.pouches.Ref(changed)
		ref.Items.Set(0, 7)
		row, _ := lw.shelves.Ref(changed)
		row.Rows.Set(0, shelfRow{Items: NewList[uint32](7, 8)})
		lw.unpack.From(removed)
		lw.unshelve.From(removed)
	})

	caught := map[string]string{}
	var keptPouches []pouch
	var keptShelves []shelf
	w.reader(t, func(pouches *Hooks[pouch, HookAll], shelves *Hooks[shelf, HookAll]) {
		for _, hook := range pouches.All() {
			caught["flat "+hookKindOf(hook)] = recovered(func() { hook.Value.Items.Set(0, 99) })
			keptPouches = append(keptPouches, hook.Value)
		}
		for _, hook := range shelves.All() {
			items := hook.Value.Rows.At(0).Items
			caught["nested "+hookKindOf(hook)] = recovered(func() { items.Set(0, 99) })
			keptShelves = append(keptShelves, hook.Value)
		}
	})
	w.reader(t, func(*Hooks[pouch, HookAll], *Hooks[shelf, HookAll]) {
		for i, kept := range keptPouches {
			caught[fmt.Sprintf("flat kept %d", i)] = recovered(func() { kept.Items.Set(1, 99) })
		}
		for i, kept := range keptShelves {
			items := kept.Rows.At(0).Items
			caught[fmt.Sprintf("nested kept %d", i)] = recovered(func() { items.Set(1, 99) })
		}
	})

	if len(caught) != 12 {
		t.Fatalf("the reader tried %d Sets, want 12: %v", len(caught), caught)
	}
	for what, message := range caught {
		component := "pouch"
		if strings.HasPrefix(what, "nested") {
			component = "shelf"
		}
		for _, want := range []string{
			"Hooks[ecs." + component + ", ecs.HookAll]", "hookListReader", "Set[ecs." + component + "].Ref",
		} {
			if !strings.Contains(message, want) {
				t.Errorf("Set through a %s Hook value: the panic does not name %q: %q", what, want, message)
			}
		}
	}
}

// TestAHookValueIsTheReadersOwnCopy is the other half: assigning a Hook value's
// plain fields, or replacing a List header in it, changes only the reader's
// copy, and a Set on a List the reader built itself is its own business.
func TestAHookValueIsTheReadersOwnCopy(t *testing.T) {
	w := newHookListWorld(t)
	e := w.entities.alloc()
	w.writer(t, func(lw listWrite) {
		lw.pouches.UpdateFor(e, pouch{Items: NewList[uint32](1, 2), Count: 2})
	})
	var caught string
	var count uint32
	w.reader(t, func(pouches *Hooks[pouch, HookAll], _ *Hooks[shelf, HookAll]) {
		for _, hook := range pouches.All() {
			caught = recovered(func() {
				hook.Value.Count = 9
				hook.Value.Items = NewList[uint32](5, 6)
				hook.Value.Items.Set(0, 99)
			})
			count = hook.Value.Count
		}
	})
	if caught != "" {
		t.Fatalf("writing the reader's own copy of a Hook value was refused: %s", caught)
	}
	if count != 9 {
		t.Fatalf("the assignment did not land in the reader's copy: Count is %d", count)
	}
	value, _ := w.lists.pouches.Get(e)
	var stored []uint32
	for _, item := range value.Items.All() {
		stored = append(stored, item)
	}
	if value.Count != 2 || fmt.Sprint(stored) != "[1 2]" {
		t.Fatalf("writing the reader's copy reached the Store: count %d, items %v", value.Count, stored)
	}
}

// holdsItems gives e a pouch with a List of its own and, in a writer's run that
// writes its row, makes the compare at that run's end register the List to it.
func (w *hookListWorld) holdsItems(t *testing.T, owners ...Entity) {
	t.Helper()
	for _, e := range owners {
		w.lists.pouches.Set(e, pouch{Items: NewList[uint32](1, 2)})
	}
	w.writer(t, func(lw listWrite) {
		for _, e := range owners {
			ref, _ := lw.pouches.Ref(e)
			ref.Count++
		}
	})
}

// setThroughRef is a Set on e's stored List, through Ref, and what it panicked
// with.
func (w *hookListWorld) setThroughRef(t *testing.T, e Entity) (caught string) {
	t.Helper()
	w.writer(t, func(lw listWrite) {
		ref, _ := lw.pouches.Ref(e)
		caught = recovered(func() { ref.Items.Set(0, 9) })
	})
	return caught
}

// TestASecondOwnerSetIsCaught is ecs.md § Validation mode's second owner: on a
// Store a Changed reader watches, a List that arrives in a second Component
// while its first owner still holds it is shared, and a Set on it through
// either Component panics naming both.
func TestASecondOwnerSetIsCaught(t *testing.T) {
	w := newHookListWorld(t)
	first, second := w.entities.alloc(), w.entities.alloc()
	w.holdsItems(t, first, second)
	w.writer(t, func(lw listWrite) {
		value, _ := lw.pouches.Of(first)
		ref, _ := lw.pouches.Ref(second)
		ref.Items = value.Items
	})

	for _, e := range []Entity{first, second} {
		caught := w.setThroughRef(t, e)
		for _, want := range []string{"ecs.pouch of " + first.String(), "ecs.pouch of " + second.String()} {
			if !strings.Contains(caught, want) {
				t.Fatalf("a Set through %v on a List two Components hold does not name %q: %q", e, want, caught)
			}
		}
	}
}

// TestTheFirstOwnerStopsHoldingAListAtTheActThatRemovesIt is hooks.md § Reading
// a retained List: the first owner stops holding a List at Remove.From or a
// Despawn, and a Hook log retaining the removal is never an owner, so a List
// moved on to another Entity is not shared while a reader still retains the
// removal. A List handed back and forth between two Entities is not shared
// either, because each stops holding what it no longer holds.
func TestTheFirstOwnerStopsHoldingAListAtTheActThatRemovesIt(t *testing.T) {
	w := newHookListWorld(t)
	removed, despawned, swapped := w.entities.alloc(), w.entities.alloc(), w.entities.alloc()
	toRemoved, toDespawned, toSwapped := w.entities.alloc(), w.entities.alloc(), w.entities.alloc()
	w.holdsItems(t, removed, despawned, swapped, toRemoved, toDespawned, toSwapped)
	w.writer(t, func(lw listWrite) {
		move := func(from, to Entity) {
			value, _ := lw.pouches.Of(from)
			ref, _ := lw.pouches.Ref(to)
			ref.Items = value.Items
		}
		move(removed, toRemoved)
		lw.unpack.From(removed)
		move(despawned, toDespawned)
		lw.we.Despawn(despawned)
		held, _ := lw.pouches.Of(toSwapped)
		move(swapped, toSwapped)
		ref, _ := lw.pouches.Ref(swapped)
		ref.Items = held.Items
	})
	if retained := len(w.lists.pouches.hooks.retained); retained != 2 {
		t.Fatalf("the log retains %d removed values, want the 2 no reader has passed", retained)
	}

	for _, e := range []Entity{toRemoved, toDespawned, swapped, toSwapped} {
		if caught := w.setThroughRef(t, e); caught != "" {
			t.Errorf("a Set on the List %v holds alone was refused: %s", e, caught)
		}
	}
}
