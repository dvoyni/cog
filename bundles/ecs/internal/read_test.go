package internal

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
)

// The read fixture's Components: one of each shape a Component may take.
type (
	spot     struct{ X, Y float32 }
	label    struct{ Text string }
	waypoint struct{ X, Y int32 }
	route    struct{ Stops m.List[waypoint] }
	follows  struct{ Target ecs.Entity }
	sprite   struct{ Pixels assets.Blob }
	marked   struct{}
	reading  struct{ V float64 }
)

// The Component sets the fixture spawns.
type (
	plainSet    struct{ Spot spot }
	labelledSet struct {
		Spot  spot
		Label label
	}
	fullSet struct {
		Spot    spot
		Label   label
		Route   route
		Follows follows
		Sprite  sprite
		Marked  marked
	}
	brokenSet struct {
		Spot    spot
		Reading reading
	}
)

// spawnCmd spawns one Entity per element of its request and answers their
// handles, in order.
type spawnCmd[S any] kernel.Command[[]S, []ecs.Entity]

type despawnCmd kernel.Command[[]ecs.Entity, struct{}]

// stripCmd takes a spot away from an Entity, which leaves a plainSet Entity
// alive with no Components.
type stripCmd kernel.Command[ecs.Entity, struct{}]

type (
	rerouteEvent struct{}
	stallEvent   struct{}
)

type (
	rerouteSystem kernel.Subscription[rerouteEvent]
	stallSystem   kernel.Subscription[stallEvent]
)

// readFixture is a game with one Component of every shape, commands that spawn
// and despawn from outside the frame, a System that writes a List in place, and
// a System that blocks while it holds its Query's read{*ecs.Entities}.
type readFixture struct {
	entered chan struct{}
	release chan struct{}
}

func newReadFixture() *readFixture {
	return &readFixture{entered: make(chan struct{}, 1), release: make(chan struct{})}
}

func (*readFixture) Name() kernel.PluginName { return "reads" }

func (*readFixture) Dependencies() []kernel.PluginName { return []kernel.PluginName{ecs.Name} }

func (f *readFixture) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[spot](registrar, 8)
	ecs.RegisterComponent[label](registrar, 8)
	ecs.RegisterComponent[route](registrar, 8)
	ecs.RegisterComponent[follows](registrar, 8)
	ecs.RegisterComponent[sprite](registrar, 8)
	ecs.RegisterComponent[marked](registrar, 8)
	ecs.RegisterComponent[reading](registrar, 8)
	registerSpawn[plainSet](registrar)
	registerSpawn[labelledSet](registrar)
	registerSpawn[fullSet](registrar)
	registerSpawn[brokenSet](registrar)
	registrar.HandleCommand[despawnCmd](ecs.ToExecute[[]ecs.Entity, struct{}](registrar, func(doomed []ecs.Entity, we *ecs.WriteableEntities) {
		for _, e := range doomed {
			we.Despawn(e)
		}
	}))
	registrar.HandleCommand[stripCmd](ecs.ToExecute[ecs.Entity, struct{}](registrar, func(e ecs.Entity, remove *ecs.Remove[spot]) {
		remove.From(e)
	}))
	registrar.Subscribe[rerouteSystem](ecs.ToHandler[rerouteEvent](registrar, func(q *ecs.Query[struct{ Route *route }]) {
		for _, it := range q.All() {
			if it.Route.Stops.Len() > 0 {
				it.Route.Stops.Set(0, waypoint{X: 99, Y: 99})
			}
		}
	}))
	registrar.Subscribe[stallSystem](ecs.ToHandler[stallEvent](registrar, func(q *ecs.Query[struct{ Spot spot }]) {
		f.entered <- struct{}{}
		<-f.release
	}))
	return nil
}

func registerSpawn[S any](registrar *kernel.Registrar) {
	registrar.HandleCommand[spawnCmd[S]](ecs.ToExecute[[]S, []ecs.Entity](registrar, func(sets []S, sp *ecs.Spawn[S], answer *ecs.Resp[[]ecs.Entity]) {
		spawned := make([]ecs.Entity, len(sets))
		for i, set := range sets {
			spawned[i] = sp.New(set)
		}
		answer.Set(spawned)
	}))
}

func spawn[S any](executioner kernel.Executioner, sets ...S) []ecs.Entity {
	return executioner.ExecuteCommand[spawnCmd[S]](sets)
}

func startReads(t *testing.T) (*kernel.Engine, *readFixture) {
	t.Helper()
	fixture := newReadFixture()
	return runEngine(t, New(), fixture), fixture
}

// decoded round-trips a response through JSON, the way a tool outside Go
// receives it.
func decoded(t *testing.T, response any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("the response does not encode: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("the response %s does not decode: %v", encoded, err)
	}
	return document
}

func name[T any]() string { return kernel.TypeName(reflect.TypeFor[T]()) }

func componentNames(values []types.ComponentValue) []string {
	names := make([]string, len(values))
	for i, value := range values {
		names[i] = value.Name
	}
	return names
}

func valueOf(t *testing.T, values []types.ComponentValue, component string) types.ComponentValue {
	t.Helper()
	for _, value := range values {
		if value.Name == component {
			return value
		}
	}
	t.Fatalf("no %s among %v", component, componentNames(values))
	return types.ComponentValue{}
}

func TestTheCensusNamesEveryComponentWithItsPopulation(t *testing.T) {
	engine, _ := startReads(t)
	executioner := engine.Executioner()
	plain := spawn(executioner, make([]plainSet, 200)...)
	spawn(executioner, make([]labelledSet, 100)...)

	census := executioner.ExecuteCommand[censusCmd](types.CensusRequest{})
	if census.Refusal != "" {
		t.Fatalf("the census was refused: %s", census.Refusal)
	}
	if census.Entities != 300 || census.FreeIndices != 0 || census.IndexSpace != 300 {
		t.Fatalf("the census reports %d alive, %d free, %d indices; want 300, 0, 300", census.Entities, census.FreeIndices, census.IndexSpace)
	}
	populations := map[string]int{}
	var names []string
	for _, component := range census.Components {
		populations[component.Name] = component.Population
		names = append(names, component.Name)
	}
	if !slices.IsSorted(names) {
		t.Errorf("the Components are not sorted by name: %v", names)
	}
	want := map[string]int{
		name[spot](): 300, name[label](): 100, name[route](): 0, name[follows](): 0,
		name[sprite](): 0, name[marked](): 0, name[reading](): 0, name[m.Transform](): 0,
	}
	for component, population := range want {
		got, ok := populations[component]
		if !ok {
			t.Errorf("the census does not name %s among %v", component, names)
		} else if got != population {
			t.Errorf("%s has population %d, want %d", component, got, population)
		}
	}
	if !strings.HasPrefix(name[spot](), "ecs.") || populations["m.Transform"] != 0 {
		t.Errorf("the names are not kernel.TypeName's: %v", names)
	}

	executioner.ExecuteCommand[despawnCmd](plain[:7])
	census = executioner.ExecuteCommand[censusCmd](types.CensusRequest{})
	if census.Entities != 293 || census.FreeIndices != 7 || census.IndexSpace != 300 {
		t.Fatalf("after 7 despawns the census reports %d alive, %d free, %d indices; want 293, 7, 300", census.Entities, census.FreeIndices, census.IndexSpace)
	}
	document := decoded(t, census)
	for _, field := range []string{"entities", "freeIndices", "indexSpace", "components"} {
		if _, ok := document[field]; !ok {
			t.Errorf("the encoded response has no %q: %v", field, document)
		}
	}
}

func TestTheEntityReadAnswersEveryComponentAsJSON(t *testing.T) {
	engine, _ := startReads(t)
	executioner := engine.Executioner()
	target := spawn(executioner, plainSet{Spot: spot{X: 1}})[0]
	e := spawn(executioner, fullSet{
		Spot:    spot{X: 3, Y: 4},
		Label:   label{Text: "hero"},
		Route:   route{Stops: m.NewList(waypoint{1, 2}, waypoint{3, 4})},
		Follows: follows{Target: target},
		Sprite:  sprite{Pixels: assets.NewBlobFromString("pixels")},
	})[0]

	index, generation := entityHalves(t, e)
	forms := []string{
		index + "v" + generation,
		"Entity(" + index + "v" + generation + ")",
		strconv.FormatUint(uint64(e), 10),
	}
	var first types.EntityResponse
	for i, form := range forms {
		answer := executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: form})
		if answer.Refusal != "" {
			t.Fatalf("%q was refused: %s", form, answer.Refusal)
		}
		if i == 0 {
			first = answer
		} else if !reflect.DeepEqual(answer, first) {
			t.Errorf("%q answered %+v, want what %q answered, %+v", form, answer, forms[0], first)
		}
	}
	if first.Entity != e.String() {
		t.Errorf("the answer names %s, want %s", first.Entity, e)
	}
	names := componentNames(first.Components)
	wantNames := []string{name[follows](), name[label](), name[marked](), name[route](), name[spot](), name[sprite]()}
	slices.Sort(wantNames)
	if !slices.Equal(names, wantNames) {
		t.Fatalf("the Entity carries %v, want %v sorted", names, wantNames)
	}

	document := decoded(t, first)
	encodedValue := func(component string) any {
		for _, c := range document["components"].([]any) {
			if c.(map[string]any)["name"] == component {
				return c.(map[string]any)["value"]
			}
		}
		t.Fatalf("the encoded answer has no %s", component)
		return nil
	}
	if got, want := encodedValue(name[route]()), []any{map[string]any{"X": 1.0, "Y": 2.0}, map[string]any{"X": 3.0, "Y": 4.0}}; !reflect.DeepEqual(got.(map[string]any)["Stops"], want) {
		t.Errorf("the List arrived as %v, want the array %v", got, want)
	}
	if got := encodedValue(name[sprite]()); !reflect.DeepEqual(got, map[string]any{"Pixels": map[string]any{"len": 6.0}}) {
		t.Errorf("the Blob arrived as %v, want {\"len\":6}", got)
	}
	if got := encodedValue(name[marked]()); !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("the Tag arrived as %#v, want an empty object", got)
	}
	if got := encodedValue(name[label]()); !reflect.DeepEqual(got, map[string]any{"Text": "hero"}) {
		t.Errorf("the string arrived as %v", got)
	}

	// The Reference is exact, not a float64 rounding of it, and feeding its
	// decimal form back resolves the Entity it refers to.
	reference := valueOf(t, first.Components, name[follows]()).Value.(map[string]any)["Target"]
	number, ok := reference.(json.Number)
	if !ok || number.String() != strconv.FormatUint(uint64(target), 10) {
		t.Fatalf("the Reference arrived as %#v, want the json.Number %d", reference, uint64(target))
	}
	back := executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: number.String()})
	if back.Refusal != "" || back.Entity != target.String() {
		t.Fatalf("the Reference fed back answered %+v, want %s", back, target)
	}
	if !slices.Equal(componentNames(back.Components), []string{name[spot]()}) {
		t.Errorf("the target carries %v, want only %s", componentNames(back.Components), name[spot]())
	}
}

func TestAnUnencodableValueFailsOnlyItsComponent(t *testing.T) {
	engine, _ := startReads(t)
	executioner := engine.Executioner()
	e := spawn(executioner, brokenSet{Spot: spot{X: 1}, Reading: reading{V: math.NaN()}})[0]
	answer := executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: e.String()})
	if answer.Refusal != "" {
		t.Fatalf("the Entity was refused: %s", answer.Refusal)
	}
	broken := valueOf(t, answer.Components, name[reading]())
	if broken.Error == "" || broken.Value != nil {
		t.Errorf("the NaN reading answered %+v, want an error and no value", broken)
	}
	fine := valueOf(t, answer.Components, name[spot]())
	if fine.Error != "" || fine.Value == nil {
		t.Errorf("the spot beside it answered %+v, want its value", fine)
	}
	if _, err := json.Marshal(answer); err != nil {
		t.Errorf("the answer carrying the error does not encode: %v", err)
	}
}

func TestTheEntityReadRefusesWhatIsNotAlive(t *testing.T) {
	engine, _ := startReads(t)
	executioner := engine.Executioner()
	spawned := spawn(executioner, make([]plainSet, 8)...)
	gone := spawned[7]
	executioner.ExecuteCommand[despawnCmd]([]ecs.Entity{gone})
	index, generation := entityHalves(t, gone)
	nextGeneration, _ := strconv.Atoi(generation)
	next := index + "v" + strconv.Itoa(nextGeneration+1)

	// Before the index is reused, nothing holds it: the despawned handle names
	// no holder, and the handle the index will carry next is not alive either,
	// although Alive alone would say it is.
	answer := executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: gone.String()})
	if answer.Refusal != gone.String()+" is not alive" {
		t.Errorf("the despawned Entity answered %+v, want it refused as not alive and no holder named", answer)
	}
	if answer.Entity != "" || answer.Components != nil {
		t.Errorf("a refusal carries %+v besides the refusal", answer)
	}
	answer = executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: next})
	if answer.Refusal == "" || strings.Contains(answer.Refusal, "holds") {
		t.Errorf("the fabricated next handle %s answered %+v, want it refused as not alive and no holder named", next, answer)
	}

	reused := spawn(executioner, plainSet{})[0]
	if reused.String() != "Entity("+next+")" {
		t.Fatalf("the respawn got %s, want the freed index at its next generation, Entity(%s)", reused, next)
	}
	answer = executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: gone.String()})
	if want := gone.String() + " is not alive; index " + index + " now holds " + reused.String(); answer.Refusal != want {
		t.Errorf("the despawned Entity answered %q, want %q", answer.Refusal, want)
	}
	answer = executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: next})
	if answer.Refusal != "" || answer.Entity != reused.String() {
		t.Errorf("the reused handle answered %+v, want it", answer)
	}

	for _, malformed := range []string{"", "seven", "7v", "v2", "Entity(7v2", "7v2v3", "-1", "NoEntity", "0", "0v0", "99999999999v1"} {
		answer := executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: malformed})
		if answer.Refusal == "" {
			t.Errorf("%q answered %+v, want it refused", malformed, answer)
		}
	}
	answer = executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: "12345v1"})
	if answer.Refusal != "Entity(12345v1) is not alive" {
		t.Errorf("an index beyond the index space answered %+v, want it refused as not alive", answer)
	}
}

func TestAnEntityWithNoComponentsAnswersAnEmptyList(t *testing.T) {
	engine, _ := startReads(t)
	executioner := engine.Executioner()
	e := spawn(executioner, plainSet{})[0]
	executioner.ExecuteCommand[stripCmd](e)
	answer := executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: e.String()})
	if answer.Refusal != "" || answer.Components == nil || len(answer.Components) != 0 {
		t.Fatalf("an alive Entity with no Components answered %+v, want an empty list", answer)
	}
	if got := decoded(t, answer)["components"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("its components encoded as %#v, want []", got)
	}
}

func TestTheQueryReadWalksTheNamedComponents(t *testing.T) {
	engine, _ := startReads(t)
	executioner := engine.Executioner()
	spawn(executioner, make([]plainSet, 150)...)
	labelled := make([]labelledSet, 150)
	for i := range labelled {
		labelled[i] = labelledSet{Spot: spot{X: float32(i)}, Label: label{Text: strconv.Itoa(i)}}
	}
	withLabels := spawn(executioner, labelled...)
	// Despawn some from the middle and respawn, so ascending index order is not
	// the drivers' dense order.
	executioner.ExecuteCommand[despawnCmd](withLabels[10:20])
	spawn(executioner, make([]labelledSet, 10)...)

	query := executioner.ExecuteCommand[queryCmd](types.QueryRequest{Components: []string{name[spot](), name[label]()}})
	if query.Refusal != "" {
		t.Fatalf("the query was refused: %s", query.Refusal)
	}
	if query.Total != 150 || !query.Truncated || len(query.Entities) != 50 {
		t.Fatalf("the query answered total %d, truncated %v, %d Entities; want 150, true, 50 by default", query.Total, query.Truncated, len(query.Entities))
	}
	all := executioner.ExecuteCommand[queryCmd](types.QueryRequest{Components: []string{name[label](), name[spot]()}, Limit: 500})
	if all.Total != 150 || all.Truncated || len(all.Entities) != 150 {
		t.Fatalf("limit 500 answered total %d, truncated %v, %d Entities; want 150, false, 150", all.Total, all.Truncated, len(all.Entities))
	}
	indices := make([]int, len(all.Entities))
	for i, entity := range all.Entities {
		e, err := strconv.ParseUint(strings.TrimPrefix(strings.Split(entity.Entity, "v")[0], "Entity("), 10, 32)
		if err != nil {
			t.Fatalf("%s is not an Entity string", entity.Entity)
		}
		indices[i] = int(e)
		if got := componentNames(entity.Components); !slices.Equal(got, []string{name[label](), name[spot]()}) {
			t.Fatalf("%s carries %v, want only the named %s and %s", entity.Entity, got, name[label](), name[spot]())
		}
	}
	if !slices.IsSorted(indices) {
		t.Errorf("the matches are not in ascending index order: %v", indices)
	}
	if !reflect.DeepEqual(query.Entities, all.Entities[:50]) {
		t.Errorf("the default page is not the first 50 of the whole answer")
	}

	limited := executioner.ExecuteCommand[queryCmd](types.QueryRequest{Components: []string{name[spot]()}, Limit: 7})
	if limited.Total != 300 || !limited.Truncated || len(limited.Entities) != 7 {
		t.Errorf("limit 7 answered total %d, truncated %v, %d Entities; want 300, true, 7", limited.Total, limited.Truncated, len(limited.Entities))
	}
	none := executioner.ExecuteCommand[queryCmd](types.QueryRequest{Components: []string{name[route]()}})
	if none.Refusal != "" || none.Total != 0 || none.Truncated || len(none.Entities) != 0 {
		t.Errorf("a query nothing matches answered %+v, want an empty answer", none)
	}
	document := decoded(t, query)
	for _, field := range []string{"total", "truncated", "entities"} {
		if _, ok := document[field]; !ok {
			t.Errorf("the encoded response has no %q", field)
		}
	}
}

func TestTheQueryReadRefusesWhatItCannotAnswer(t *testing.T) {
	engine, _ := startReads(t)
	executioner := engine.Executioner()
	spawn(executioner, make([]plainSet, 3)...)

	over := executioner.ExecuteCommand[queryCmd](types.QueryRequest{Components: []string{name[spot]()}, Limit: 501})
	if !strings.Contains(over.Refusal, "500") || over.Total != 0 || over.Entities != nil {
		t.Errorf("limit 501 answered %+v, want a refusal naming 500 and nothing else", over)
	}
	negative := executioner.ExecuteCommand[queryCmd](types.QueryRequest{Components: []string{name[spot]()}, Limit: -1})
	if negative.Refusal == "" {
		t.Errorf("limit -1 answered %+v, want it refused", negative)
	}
	census := executioner.ExecuteCommand[censusCmd](types.CensusRequest{})
	for _, request := range []types.QueryRequest{
		{Components: []string{name[spot](), "ecs.spott"}},
		{},
	} {
		refused := executioner.ExecuteCommand[queryCmd](request)
		if refused.Refusal == "" {
			t.Errorf("%+v answered %+v, want it refused", request, refused)
			continue
		}
		for _, component := range census.Components {
			if !strings.Contains(refused.Refusal, component.Name) {
				t.Errorf("the refusal %q of %+v does not list %s", refused.Refusal, request, component.Name)
			}
		}
	}
	entity := executioner.ExecuteCommand[entityCmd](types.EntityRequest{Entity: "ecs.spot"})
	if entity.Refusal == "" {
		t.Errorf("a Component name read as an Entity answered %+v", entity)
	}
}

// TestAnAnswerIsDetachedFromTheStore takes an answer holding a List, then runs a
// System that writes that List in place under its own write lock: the answer
// was encoded under the read's lock, so it does not see the write.
func TestAnAnswerIsDetachedFromTheStore(t *testing.T) {
	engine, _ := startReads(t)
	executioner := engine.Executioner()
	spawn(executioner, fullSet{Route: route{Stops: m.NewList(waypoint{1, 2})}})
	before := executioner.ExecuteCommand[queryCmd](types.QueryRequest{Components: []string{name[route]()}})
	snapshot, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("the answer does not encode: %v", err)
	}
	executioner.PublishEvent(rerouteEvent{}).Wait()
	after := executioner.ExecuteCommand[queryCmd](types.QueryRequest{Components: []string{name[route]()}})
	if again, _ := json.Marshal(before); string(again) != string(snapshot) {
		t.Errorf("the earlier answer changed after the List was written: %s, was %s", again, snapshot)
	}
	if s, _ := json.Marshal(after); !strings.Contains(string(s), "99") {
		t.Errorf("the System's write never happened: %s", s)
	}
}

// TestTheReadsHoldTheAuthorityAloneAndWidenNoSystem reads the lock sets off the
// engine's description.
func TestTheReadsHoldTheAuthorityAloneAndWidenNoSystem(t *testing.T) {
	engine, _ := startReads(t)
	description := engine.Describe()
	entities := reflect.TypeFor[*ecs.Entities]()
	reads := map[reflect.Type]bool{
		reflect.TypeFor[censusCmd](): false,
		reflect.TypeFor[entityCmd](): false,
		reflect.TypeFor[queryCmd]():  false,
	}
	for _, command := range description.Commands {
		if _, ok := reads[command.Type]; !ok {
			continue
		}
		reads[command.Type] = true
		if command.Owner != ecs.Name {
			t.Errorf("%s is owned by %q, want %q", kernel.TypeName(command.Type), command.Owner, ecs.Name)
		}
		if !slices.Equal(command.Writes, []reflect.Type{entities}) || len(command.Reads) != 0 || len(command.Uses) != 0 {
			t.Errorf("%s writes %v, reads %v and uses %v; want write{*ecs.Entities} alone",
				kernel.TypeName(command.Type), command.Writes, command.Reads, command.Uses)
		}
	}
	for command, found := range reads {
		if !found {
			t.Errorf("the architecture has no %s", kernel.TypeName(command))
		}
	}

	// Every System has exactly the lock set its signature names: none takes
	// write{*ecs.Entities}, which none spawns, and the ecs plugin subscribes
	// nothing.
	want := map[reflect.Type][2][]reflect.Type{
		reflect.TypeFor[rerouteSystem](): {{entities}, {reflect.TypeFor[*ecs.Store[route]]()}},
		reflect.TypeFor[stallSystem]():   {sortedTypes(entities, reflect.TypeFor[*ecs.Store[spot]]()), {}},
	}
	for _, subscription := range description.Subscriptions {
		if subscription.Owner == ecs.Name {
			t.Errorf("the ecs plugin subscribes %s", kernel.TypeName(subscription.Type))
		}
		expected, ok := want[subscription.Type]
		if !ok {
			continue
		}
		delete(want, subscription.Type)
		if !slices.Equal(subscription.Reads, expected[0]) || !slices.Equal(subscription.Writes, expected[1]) {
			t.Errorf("%s reads %v and writes %v, want reads %v and writes %v",
				kernel.TypeName(subscription.Type), subscription.Reads, subscription.Writes, expected[0], expected[1])
		}
	}
	for missing := range want {
		t.Errorf("the architecture has no %s", kernel.TypeName(missing))
	}

	var writers []reflect.Type
	for _, resource := range description.Contention.Resources {
		if resource.Type != entities {
			continue
		}
		for _, writer := range resource.Writers {
			writers = append(writers, writer.Type)
		}
	}
	for command := range reads {
		if !slices.Contains(writers, command) {
			t.Errorf("the contention report does not list %s among *ecs.Entities' writers %v", kernel.TypeName(command), writers)
		}
	}
}

// TestAReadWaitsForARunningSystem is the stated price, demonstrated: a read
// cannot start while a System holds read{*ecs.Entities}, and returns once it
// lets go.
func TestAReadWaitsForARunningSystem(t *testing.T) {
	engine, fixture := startReads(t)
	executioner := engine.Executioner()
	spawn(executioner, plainSet{})

	published := make(chan struct{})
	go func() {
		defer close(published)
		executioner.PublishEvent(stallEvent{}).Wait()
	}()
	<-fixture.entered

	returned := make(chan types.CensusResponse, 1)
	go func() { returned <- executioner.ExecuteCommand[censusCmd](types.CensusRequest{}) }()
	select {
	case <-returned:
		t.Fatalf("the census returned while a System held read{*ecs.Entities}")
	case <-time.After(100 * time.Millisecond):
	}
	close(fixture.release)
	select {
	case census := <-returned:
		if census.Entities != 1 {
			t.Errorf("the read answered %d Entities, want 1", census.Entities)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the census never returned after the System let go")
	}
	<-published
}

func sortedTypes(types ...reflect.Type) []reflect.Type {
	slices.SortFunc(types, func(a, b reflect.Type) int { return strings.Compare(a.String(), b.String()) })
	return types
}

func entityHalves(t *testing.T, e ecs.Entity) (index, generation string) {
	t.Helper()
	inner := strings.TrimSuffix(strings.TrimPrefix(e.String(), "Entity("), ")")
	index, generation, ok := strings.Cut(inner, "v")
	if !ok {
		t.Fatalf("%s does not render as Entity(IvG)", e)
	}
	return index, generation
}
