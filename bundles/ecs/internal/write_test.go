package internal

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
)

// secret is a Component with a field the JSON never shows.
type secret struct {
	Shown  int32
	hidden int32
}

// secretCarrier gives an Entity a secret, hidden field and all, and a sprite
// when it carries bytes: only Go can set what the JSON never shows.
type secretCarrier struct {
	Entity ecs.Entity
	Secret secret
	Sprite sprite
}

type (
	stashCmd kernel.Command[secretCarrier, struct{}]
	// peekCmd answers the bytes of an Entity's route, secret, sprite, label,
	// spot and follows, as their Stores hold them.
	peekCmd kernel.Command[ecs.Entity, []string]
)

type watchEvent struct{}

type watchSystem kernel.Subscription[watchEvent]

// watchFixture is a game that reads every act on spot, route and secret through
// Hooks, the way an index or a cache built on them would, and records each
// record as one line: the Entity, its kinds and its value.
type watchFixture struct {
	mu   sync.Mutex
	seen []string
}

func (*watchFixture) Name() kernel.PluginName { return "watches" }

func (*watchFixture) Dependencies() []kernel.PluginName { return []kernel.PluginName{"reads"} }

func (f *watchFixture) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[secret](registrar, 8)
	registrar.HandleCommand[stashCmd](ecs.ToExecute[secretCarrier, struct{}](registrar, func(
		carrier secretCarrier, secrets *ecs.Set[secret], sprites *ecs.Set[sprite],
	) {
		secrets.UpdateFor(carrier.Entity, carrier.Secret)
		if carrier.Sprite.Pixels.Len() > 0 {
			sprites.UpdateFor(carrier.Entity, carrier.Sprite)
		}
	}))
	registrar.HandleCommand[peekCmd](ecs.ToExecute[ecs.Entity, []string](registrar, func(
		e ecs.Entity, routes *ecs.Get[route], secrets *ecs.Get[secret], sprites *ecs.Get[sprite],
		labels *ecs.Get[label], spots *ecs.Get[spot], follow *ecs.Get[follows], answer *ecs.Resp[[]string],
	) {
		r, _ := routes.Of(e)
		s, _ := secrets.Of(e)
		p, _ := sprites.Of(e)
		l, _ := labels.Of(e)
		o, _ := spots.Of(e)
		f, _ := follow.Of(e)
		answer.Set([]string{rowBytes(r), rowBytes(s), rowBytes(p), rowBytes(l), rowBytes(o), rowBytes(f)})
	}))
	registrar.Subscribe[watchSystem](ecs.ToHandler[watchEvent](registrar, func(
		spots *ecs.Hooks[spot, ecs.HookAll], routes *ecs.Hooks[route, ecs.HookAll], secrets *ecs.Hooks[secret, ecs.HookAll],
		removals *ecs.Hooks[spot, ecs.HookRemoved],
	) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for e, hook := range spots.All() {
			f.seen = append(f.seen, fmt.Sprintf("%v spot %s %v", e, kinds(hook), hook.Value))
		}
		for e, hook := range routes.All() {
			f.seen = append(f.seen, fmt.Sprintf("%v route %s %v", e, kinds(hook), slices.Collect(listValues(hook.Value.Stops))))
		}
		for e, hook := range secrets.All() {
			f.seen = append(f.seen, fmt.Sprintf("%v secret %s %v", e, kinds(hook), hook.Value))
		}
		for e, hook := range removals.All() {
			f.seen = append(f.seen, fmt.Sprintf("%v removal of spot %v", e, hook.Value))
		}
	}))
	return nil
}

// records runs the watching System once and answers what it was given.
func (f *watchFixture) records(executioner kernel.Executioner) []string {
	executioner.PublishEvent(watchEvent{}).Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := f.seen
	f.seen = nil
	return seen
}

func kinds[T any](hook *ecs.Hook[T]) string {
	var named []string
	for _, kind := range []struct {
		name string
		is   bool
	}{
		{"spawned", hook.IsSpawned()}, {"despawned", hook.IsDespawned()}, {"added", hook.IsAdded()},
		{"removed", hook.IsRemoved()}, {"changed", hook.IsChanged()},
	} {
		if kind.is {
			named = append(named, kind.name)
		}
	}
	return strings.Join(named, "+")
}

func listValues[T any](l m.List[T]) func(func(T) bool) {
	return func(yield func(T) bool) {
		for _, v := range l.All() {
			if !yield(v) {
				return
			}
		}
	}
}

func startWrites(t *testing.T) (*kernel.Engine, *watchFixture) {
	t.Helper()
	watch := &watchFixture{}
	engine := runEngine(t, New(), newReadFixture(), watch)
	watch.records(engine.Executioner())
	return engine, watch
}

// request decodes a request from the JSON an agent sends, the way the broker
// does.
func request[T any](t *testing.T, document string) T {
	t.Helper()
	var decoded T
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("the request %s does not decode: %v", document, err)
	}
	return decoded
}

func entityOf(t *testing.T, executioner kernel.Executioner, e string) types.EntityResponse {
	t.Helper()
	answer := executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: e})
	if answer.Refusal != "" {
		t.Fatalf("reading %s was refused: %s", e, answer.Refusal)
	}
	return answer
}

// given checks what the Hook readers were given, in order.
func given(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("the readers were given\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func encodedValue(t *testing.T, value types.ComponentValue) string {
	t.Helper()
	encoded, err := json.Marshal(value.Value)
	if err != nil {
		t.Fatalf("%s does not encode: %v", value.Name, err)
	}
	return string(encoded)
}

func TestASpawnByNameIsASpawnToEveryHookReader(t *testing.T) {
	engine, watch := startWrites(t)
	executioner := engine.Executioner()

	spawned := executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t, fmt.Sprintf(
		`{"components": {%q: {"X": 1.5, "Y": -2}, %q: {"Stops": [{"X": 1, "Y": 2}]}, %q: {}}}`,
		name[spot](), name[route](), name[marked]())))
	if spawned.Refusal != "" {
		t.Fatalf("the spawn was refused: %s", spawned.Refusal)
	}

	read := entityOf(t, executioner, spawned.Entity)
	if got, want := componentNames(read.Components), []string{name[marked](), name[route](), name[spot]()}; !slices.Equal(got, want) {
		t.Fatalf("the spawned Entity carries %v, want %v", got, want)
	}
	if got := encodedValue(t, valueOf(t, read.Components, name[spot]())); got != `{"X":1.5,"Y":-2}` {
		t.Errorf("spot is %s", got)
	}
	if got := encodedValue(t, valueOf(t, read.Components, name[route]())); got != `{"Stops":[{"X":1,"Y":2}]}` {
		t.Errorf("route is %s", got)
	}

	want := []string{
		spawned.Entity + " spot spawned+added+changed {1.5 -2}",
		spawned.Entity + " route spawned+added+changed [{1 2}]",
	}
	given(t, watch.records(executioner), want...)
}

func TestADespawnByNameIsADespawnToEveryHookReader(t *testing.T) {
	engine, watch := startWrites(t)
	executioner := engine.Executioner()
	e := spawn(executioner, labelledSet{Spot: spot{X: 3, Y: 4}, Label: label{Text: "doomed"}})[0]
	watch.records(executioner)

	answer := executioner.ExecuteCommand[despawnCmd](types.DespawnRequest{Entity: e.String()})
	if answer.Refusal != "" || !answer.WasAlive || answer.Entity != e.String() {
		t.Fatalf("the despawn answered %+v", answer)
	}
	want := []string{
		e.String() + " spot despawned+removed {3 4}",
		e.String() + " removal of spot {3 4}",
	}
	given(t, watch.records(executioner), want...)

	again := executioner.ExecuteCommand[despawnCmd](types.DespawnRequest{Entity: e.String()})
	if again.Refusal != "" || again.WasAlive {
		t.Errorf("despawning it again answered %+v; want wasAlive false and no refusal", again)
	}
	if got := watch.records(executioner); len(got) != 0 {
		t.Errorf("despawning a dead Entity recorded %v", got)
	}
}

func update(t *testing.T, executioner kernel.Executioner, document string) types.UpdateResponse {
	t.Helper()
	return executioner.ExecuteCommand[updateCmd](request[types.UpdateRequest](t, document))
}

func outcomes(answer types.UpdateResponse) []string {
	got := make([]string, len(answer.Components))
	for i, c := range answer.Components {
		got[i] = c.Name + " " + c.Outcome
	}
	return got
}

// An update merges: a Component the Entity carries takes the fields given and
// keeps the rest, one it lacks is added, and one removed is recorded with its
// last value, each exactly as the same act from a System would be.
func TestAnUpdateByNameAddsChangesAndRemovesAsASystemWould(t *testing.T) {
	engine, watch := startWrites(t)
	executioner := engine.Executioner()
	e := spawn(executioner, plainSet{Spot: spot{X: 1, Y: 2}})[0]
	watch.records(executioner)

	answer := update(t, executioner, fmt.Sprintf(
		`{"entity": %q, "set": {%q: {"X": 9}, %q: {"Stops": [{"X": 5, "Y": 6}]}}, "remove": [%q]}`,
		e.String(), name[spot](), name[route](), name[label]()))
	if answer.Refusal != "" {
		t.Fatalf("the update was refused: %s", answer.Refusal)
	}
	if got, want := outcomes(answer), []string{name[label]() + " absent", name[route]() + " added", name[spot]() + " changed"}; !slices.Equal(got, want) {
		t.Errorf("the update answered %v, want %v", got, want)
	}
	if got := encodedValue(t, valueOf(t, entityOf(t, executioner, e.String()).Components, name[spot]())); got != `{"X":9,"Y":2}` {
		t.Errorf("spot is %s; the field not given must keep its value", got)
	}
	given(t, watch.records(executioner),
		e.String()+" spot changed {9 2}",
		e.String()+" route added+changed [{5 6}]")

	answer = update(t, executioner, fmt.Sprintf(`{"entity": %q, "remove": [%q, %q]}`, e.String(), name[route](), name[spot]()))
	if got, want := outcomes(answer), []string{name[route]() + " removed", name[spot]() + " removed"}; !slices.Equal(got, want) {
		t.Errorf("the removal answered %v (refusal %q), want %v", got, answer.Refusal, want)
	}
	given(t, watch.records(executioner),
		e.String()+" spot removed {9 2}",
		e.String()+" route removed [{5 6}]",
		e.String()+" removal of spot {9 2}")
}

// rowBytes is a Component's bytes as its Store holds them.
func rowBytes[T any](value T) string {
	return string(unsafe.Slice((*byte)(unsafe.Pointer(&value)), unsafe.Sizeof(value)))
}

// A value read with ecs_entity and sent back unedited is left as it was, to
// the byte, and records nothing - a List's array and a Blob's bytes included,
// though the decode builds a new array and cannot carry the bytes at all.
func TestAnUneditedRoundTripWritesNothing(t *testing.T) {
	engine, watch := startWrites(t)
	executioner := engine.Executioner()
	target := spawn(executioner, plainSet{Spot: spot{X: 1, Y: 1}})[0]
	e := spawn(executioner, fullSet{
		Spot: spot{X: 0.1, Y: 3e38}, Label: label{Text: "héllo \"there\""},
		Route:   route{Stops: m.NewList(waypoint{X: 1, Y: -1}, waypoint{X: 2, Y: -2})},
		Follows: follows{Target: target}, Sprite: sprite{Pixels: assets.NewBlobFromString("pixels")},
	})[0]
	executioner.ExecuteCommand[stashCmd](secretCarrier{Entity: e, Secret: secret{Shown: 1, hidden: 7}})
	watch.records(executioner)
	before := executioner.ExecuteCommand[peekCmd](e)

	values := map[string]any{}
	for _, value := range entityOf(t, executioner, e.String()).Components {
		values[value.Name] = value.Value
	}
	document, _ := json.Marshal(map[string]any{"entity": e.String(), "set": values})
	answer := update(t, executioner, string(document))
	if answer.Refusal != "" {
		t.Fatalf("the round trip was refused: %s", answer.Refusal)
	}
	for _, outcome := range answer.Components {
		if outcome.Outcome != "unchanged" {
			t.Errorf("%s came back %s, want unchanged", outcome.Name, outcome.Outcome)
		}
	}
	after := executioner.ExecuteCommand[peekCmd](e)
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("row %d changed in the Store:\n%q\nbecame\n%q", i, before[i], after[i])
		}
	}
	given(t, watch.records(executioner))
}

// Merge keeps what the JSON cannot show: an unexported field keeps its value
// on an update, a Blob keeps its bytes whatever len is given, and a Blob given
// on a spawn is empty, because its bytes never cross.
func TestAnUpdateKeepsWhatTheJSONCannotShow(t *testing.T) {
	engine, watch := startWrites(t)
	executioner := engine.Executioner()
	e := spawn(executioner, plainSet{Spot: spot{}})[0]
	executioner.ExecuteCommand[stashCmd](secretCarrier{
		Entity: e, Secret: secret{Shown: 1, hidden: 7}, Sprite: sprite{Pixels: assets.NewBlobFromString("pixels")},
	})
	watch.records(executioner)

	answer := update(t, executioner, fmt.Sprintf(`{"entity": %q, "set": {%q: {"Shown": 2}, %q: {"Pixels": {"len": 99}}}}`,
		e.String(), name[secret](), name[sprite]()))
	if got, want := outcomes(answer), []string{name[secret]() + " changed", name[sprite]() + " unchanged"}; !slices.Equal(got, want) {
		t.Fatalf("the update answered %v (refusal %q), want %v", got, answer.Refusal, want)
	}
	kept := executioner.ExecuteCommand[peekCmd](e)
	if want := rowBytes(secret{Shown: 2, hidden: 7}); kept[1] != want {
		t.Errorf("secret became %q, want Shown 2 with hidden 7 kept", kept[1])
	}
	given(t, watch.records(executioner), e.String()+" secret changed {2 7}")

	spawned := executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t, fmt.Sprintf(
		`{"components": {%q: {"Pixels": {"len": 6}}, %q: {"Shown": 3}}}`, name[sprite](), name[secret]())))
	if spawned.Refusal != "" {
		t.Fatalf("the spawn was refused: %s", spawned.Refusal)
	}
	if got := encodedValue(t, valueOf(t, entityOf(t, executioner, spawned.Entity).Components, name[sprite]())); got != `{"Pixels":{"len":0}}` {
		t.Errorf("a spawned Blob is %s, want empty", got)
	}
}

// A request with one bad entry among good ones applies nothing, and its
// refusal names every entry that failed, each with what would have worked.
func TestAWriteIsAllOrNothingAndARefusalSaysWhatWouldHaveWorked(t *testing.T) {
	engine, watch := startWrites(t)
	executioner := engine.Executioner()
	e := spawn(executioner, plainSet{Spot: spot{X: 1, Y: 2}})[0]
	gone := spawn(executioner, plainSet{})[0]
	executioner.ExecuteCommand[despawnAllCmd]([]ecs.Entity{gone})
	reused := spawn(executioner, plainSet{})[0]
	watch.records(executioner)
	census := executioner.ExecuteCommand[censusCmd](types.CensusRequest{})

	cases := []struct {
		name     string
		refusal  string
		contains []string
	}{
		{"an unknown name", executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t,
			fmt.Sprintf(`{"components": {%q: {}, "internal.nothing": {}}}`, name[spot]()))).Refusal,
			[]string{`"internal.nothing"`, "registered: ", name[route]()}},
		{"a value of the wrong shape", executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t,
			fmt.Sprintf(`{"components": {%q: {"X": "far"}}}`, name[spot]()))).Refusal,
			[]string{name[spot](), "ecs_entity"}},
		{"a field the Component lacks", executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t,
			fmt.Sprintf(`{"components": {%q: {"X": 1, "Z": 2}}}`, name[spot]()))).Refusal,
			[]string{name[spot](), "no field Z", `{"X":1,"Y":0}`}},
		{"one Component named twice", executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t,
			fmt.Sprintf(`{"components": {%q: {}, %q: {}}}`, name[spot](), qualified[spot]()))).Refusal,
			[]string{"name it once"}},
		{"a malformed Entity", update(t, executioner, fmt.Sprintf(`{"entity": "seven", "remove": [%q]}`, name[spot]())).Refusal,
			[]string{"7v2"}},
		{"a dead Entity", update(t, executioner, fmt.Sprintf(`{"entity": %q, "remove": [%q]}`, gone.String(), name[spot]())).Refusal,
			[]string{"not alive", reused.String()}},
		{"an update naming nothing", update(t, executioner, fmt.Sprintf(`{"entity": %q}`, e.String())).Refusal,
			[]string{"at least one", name[spot]()}},
		{"one Component set and removed", update(t, executioner, fmt.Sprintf(`{"entity": %q, "set": {%q: {}}, "remove": [%q]}`,
			e.String(), name[spot](), name[spot]())).Refusal,
			[]string{"to set and to remove", "name it once"}},
		{"a good entry beside two bad ones", update(t, executioner, fmt.Sprintf(
			`{"entity": %q, "set": {%q: {"X": 50}, %q: {"Stops": 3}}, "remove": ["internal.nothing"]}`,
			e.String(), name[spot](), name[route]())).Refusal,
			[]string{"nothing was applied", name[route](), `"internal.nothing"`}},
		{"a malformed Entity to despawn", executioner.ExecuteCommand[despawnCmd](types.DespawnRequest{Entity: "Entity(x)"}).Refusal,
			[]string{"7v2"}},
	}
	for _, c := range cases {
		if c.refusal == "" {
			t.Errorf("%s was not refused", c.name)
			continue
		}
		for _, want := range c.contains {
			if !strings.Contains(c.refusal, want) {
				t.Errorf("%s was refused as %q, which does not say %q", c.name, c.refusal, want)
			}
		}
	}

	if got := encodedValue(t, valueOf(t, entityOf(t, executioner, e.String()).Components, name[spot]())); got != `{"X":1,"Y":2}` {
		t.Errorf("a refused update applied its good entry: spot is %s", got)
	}
	after := executioner.ExecuteCommand[censusCmd](types.CensusRequest{})
	if after.Entities != census.Entities || after.AgentWrites != census.AgentWrites {
		t.Errorf("refused writes changed the world: %d Entities and %d writes, were %d and %d",
			after.Entities, after.AgentWrites, census.Entities, census.AgentWrites)
	}
	given(t, watch.records(executioner))
}

// The census counts the write calls that changed the world, and only those.
func TestTheCensusCountsTheWritesThatChangedTheWorld(t *testing.T) {
	engine, _ := startWrites(t)
	executioner := engine.Executioner()
	count := func() uint64 { return executioner.ExecuteCommand[censusCmd](types.CensusRequest{}).AgentWrites }
	if got := count(); got != 0 {
		t.Fatalf("a game nobody wrote to counts %d writes", got)
	}
	spawned := executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t, fmt.Sprintf(`{"components": {%q: {"X": 1}}}`, name[spot]())))
	update(t, executioner, fmt.Sprintf(`{"entity": %q, "set": {%q: {"X": 1}}}`, spawned.Entity, name[spot]()))
	update(t, executioner, fmt.Sprintf(`{"entity": %q, "remove": [%q]}`, spawned.Entity, name[label]()))
	update(t, executioner, fmt.Sprintf(`{"entity": %q, "set": {%q: {"X": 2}}}`, spawned.Entity, name[spot]()))
	executioner.ExecuteCommand[despawnCmd](types.DespawnRequest{Entity: spawned.Entity})
	executioner.ExecuteCommand[despawnCmd](types.DespawnRequest{Entity: spawned.Entity})
	if got := count(); got != 3 {
		t.Errorf("a spawn, a change and a despawn, beside an unchanged update, an absent removal and a second despawn, counted %d writes; want 3", got)
	}
	bare := executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t, `{"components": {}}`))
	if bare.Refusal != "" || len(entityOf(t, executioner, bare.Entity).Components) != 0 {
		t.Errorf("a spawn of nothing answered %+v; want a bare Entity", bare)
	}
}

// qualified is a Component's package-qualified name, which resolves as its
// short one does.
func qualified[T any]() string {
	componentType := reflect.TypeFor[T]()
	return componentType.PkgPath() + "." + componentType.Name()
}

// A request decodes its numbers exactly, as the reads encode them, so an
// Entity Reference past 2^53 is not rounded through a float64 on its way in.
func TestAWriteCarriesAnEntityReferenceExactly(t *testing.T) {
	engine, _ := startWrites(t)
	executioner := engine.Executioner()
	const handle = "9007199254740993" // 2^53 + 1, which a float64 cannot hold
	spawned := executioner.ExecuteCommand[spawnCmd](request[types.SpawnRequest](t,
		fmt.Sprintf(`{"components": {%q: {"Target": %s}}}`, name[follows](), handle)))
	if spawned.Refusal != "" {
		t.Fatalf("the spawn was refused: %s", spawned.Refusal)
	}
	if got := encodedValue(t, valueOf(t, entityOf(t, executioner, spawned.Entity).Components, name[follows]())); got != `{"Target":`+handle+`}` {
		t.Errorf("the Reference came back %s, want %s exactly", got, handle)
	}
}
