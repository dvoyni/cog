package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
)

// Three capabilities, every one a look: an Agent may call them freely, and a
// client may auto-approve them, because none changes the game.
func TestCapabilities_ThreeLooksAndNoAct(t *testing.T) {
	capabilities := (provider{}).Capabilities()
	if len(capabilities) != 3 {
		t.Fatalf("ecs offers %d capabilities, want 3", len(capabilities))
	}
	want := map[string][2]reflect.Type{
		censusName: {reflect.TypeFor[types.CensusRequest](), reflect.TypeFor[types.CensusResponse]()},
		entityName: {reflect.TypeFor[types.EntityRequest](), reflect.TypeFor[types.EntityResponse]()},
		queryName:  {reflect.TypeFor[types.QueryRequest](), reflect.TypeFor[types.QueryResponse]()},
	}
	for _, one := range capabilities {
		if err := one.Err(); err != nil {
			t.Errorf("capability %q did not construct: %v", one.Name(), err)
		}
		shape, ok := want[one.Name()]
		if !ok {
			t.Errorf("ecs offers %q, which is none of world, entity and query", one.Name())
			continue
		}
		delete(want, one.Name())
		if !one.ReadOnly() {
			t.Errorf("%q changes nothing, so it is read-only", one.Name())
		}
		if one.RequestType() != shape[0] || one.ResponseType() != shape[1] {
			t.Errorf("%q is %v -> %v, want %v -> %v", one.Name(), one.RequestType(), one.ResponseType(), shape[0], shape[1])
		}
	}
	for missing := range want {
		t.Errorf("ecs offers no %q", missing)
	}
	if censusName != "world" || entityName != "entity" || queryName != "query" {
		t.Errorf("the capabilities are named %q, %q, %q; want world, entity, query, which render as ecs_world, ecs_entity, ecs_query",
			censusName, entityName, queryName)
	}
}

func capabilityNamed(t *testing.T, name string) mcp.Capability {
	t.Helper()
	for _, one := range (provider{}).Capabilities() {
		if one.Name() == name {
			return one
		}
	}
	t.Fatalf("ecs offers no %q", name)
	return mcp.Capability{}
}

// The descriptions are prompt text, and they carry what an Agent most often
// gets wrong: a partial list read as the whole world, a missing field read as
// a zero one, a call looped every tick in a busy game, and the tools' own
// contention pairs taken for the game's.
func TestCapabilities_TheDescriptionsCarryWhatAnAgentGetsWrong(t *testing.T) {
	for _, one := range (provider{}).Capabilities() {
		if one.Description() == "" {
			t.Fatalf("capability %q has no description", one.Name())
		}
	}
	pins := map[string][]string{
		censusName: {"ecs_entity", "ecs_query", "changes nothing", "paused", "no tick"},
		entityName: {"7v2", "Entity(7v2)", "decimal handle", "sorted by name", "not alive", "now holds",
			"array", `{"len":`, "null", "unexported", "{}", "`error`"},
		queryName: {"every", "ecs_entity", "ascending index order", "50", "500", "total", "truncated",
			"holds up", "keep `limit` small", "paused", "no tick"},
	}
	for capability, phrases := range pins {
		description := capabilityNamed(t, capability).Description()
		for _, phrase := range phrases {
			if !strings.Contains(description, phrase) {
				t.Errorf("the %s description never says %q", capability, phrase)
			}
		}
	}

	// The contention sentence names the three Commands as kernel.TypeName
	// renders them, computed rather than spelled, so a rename cannot leave the
	// prompt naming a type that is gone.
	commands := []string{
		kernel.TypeName(reflect.TypeFor[censusCmd]()),
		kernel.TypeName(reflect.TypeFor[entityCmd]()),
		kernel.TypeName(reflect.TypeFor[queryCmd]()),
	}
	for _, capability := range []string{censusName, queryName} {
		description := capabilityNamed(t, capability).Description()
		for _, phrase := range append([]string{"*ecs.Entities", "mcpserver_architecture", "ecs.ShrinkCmd"}, commands...) {
			if !strings.Contains(description, phrase) {
				t.Errorf("the %s description never says %q", capability, phrase)
			}
		}
	}
}

// agentRig collects the mcp Providers the composed plugins contribute, the way
// the broker does, so what a test calls is what an Agent calls. It composes no
// broker, no app and no backend.
type agentRig struct {
	providers kernel.CollectedAdapters[mcp.Provider]
}

func (*agentRig) Name() kernel.PluginName { return "agentrig" }

func (*agentRig) Dependencies() []kernel.PluginName { return nil }

func (r *agentRig) Register(registrar *kernel.Registrar, _ any) error {
	r.providers = registrar.CollectAdapters[mcp.ProviderPort]()
	return nil
}

// startAgentRig composes the ecs plugin, the read fixture's game and the rig,
// and answers the engine and every collected capability keyed as the broker
// names its tool.
func startAgentRig(t *testing.T) (*kernel.Engine, map[string]mcp.Capability) {
	t.Helper()
	rig := &agentRig{}
	engine := runEngine(t, New(), newReadFixture(), rig)
	tools := map[string]mcp.Capability{}
	for _, one := range rig.providers.Get() {
		for _, capability := range one.Adapter.Capabilities() {
			if err := capability.Err(); err != nil {
				t.Fatalf("capability %s: %v", capability.Name(), err)
			}
			tools[string(one.Plugin)+"_"+capability.Name()] = capability
		}
	}
	for _, tool := range []string{"ecs_world", "ecs_entity", "ecs_query"} {
		if _, offered := tools[tool]; !offered {
			t.Fatalf("no Provider contributed %s; collected %v", tool, slices.Collect(maps.Keys(tools)))
		}
	}
	return engine, tools
}

func invoke[TResponse any](t *testing.T, engine *kernel.Engine, capability mcp.Capability, request any) (TResponse, error) {
	t.Helper()
	answered, err := capability.Invoke(engine.Executioner(), request)
	if err != nil {
		var zero TResponse
		return zero, err
	}
	return answered.(TResponse), nil
}

// An Agent's three calls, through the Providers the kernel bound, over a game
// of a few hundred Entities carrying every shape a Component may take.
func TestTheToolsAnswerAnAgentThroughTheBoundProvider(t *testing.T) {
	engine, tools := startAgentRig(t)
	executioner := engine.Executioner()
	target := spawn(executioner, make([]plainSet, 200)...)[0]
	labelled := make([]labelledSet, 120)
	for i := range labelled {
		labelled[i] = labelledSet{Label: label{Text: strconv.Itoa(i)}}
	}
	spawn(executioner, labelled...)
	hero := spawn(executioner, fullSet{
		Label:   label{Text: "hero"},
		Route:   route{Stops: m.NewList(waypoint{1, 2})},
		Follows: follows{Target: target},
		Sprite:  sprite{Pixels: assets.NewBlobFromString("pixels")},
	})[0]

	census, err := invoke[types.CensusResponse](t, engine, tools["ecs_world"], &types.CensusRequest{})
	if err != nil {
		t.Fatalf("ecs_world: %v", err)
	}
	if census.Entities != 321 || census.IndexSpace != 321 || census.FreeIndices != 0 {
		t.Errorf("ecs_world reports %d alive, %d free, %d indices; want 321, 0, 321", census.Entities, census.FreeIndices, census.IndexSpace)
	}
	populations := map[string]int{}
	for _, component := range census.Components {
		populations[component.Name] = component.Population
	}
	for component, want := range map[string]int{name[spot](): 321, name[label](): 121, name[route](): 1, name[marked](): 1, name[m.Transform](): 0} {
		if got, ok := populations[component]; !ok || got != want {
			t.Errorf("ecs_world reports %s at %d (listed %v), want %d", component, got, ok, want)
		}
	}

	entity, err := invoke[types.EntityResponse](t, engine, tools["ecs_entity"], &types.EntityRequest{Entity: hero.String()})
	if err != nil {
		t.Fatalf("ecs_entity: %v", err)
	}
	if got := valueOf(t, entity.Components, name[label]()).Value; !reflect.DeepEqual(got, map[string]any{"Text": "hero"}) {
		t.Errorf("ecs_entity answers the label as %v", got)
	}
	reference := valueOf(t, entity.Components, name[follows]()).Value.(map[string]any)["Target"]
	if fmt.Sprint(reference) != strconv.FormatUint(uint64(target), 10) {
		t.Errorf("ecs_entity answers the Reference as %v, want the decimal handle %d", reference, uint64(target))
	}
	if got := valueOf(t, entity.Components, name[sprite]()).Value; !reflect.DeepEqual(got, map[string]any{"Pixels": map[string]any{"len": json.Number("6")}}) {
		t.Errorf("ecs_entity answers the Blob as %#v, want its length alone", got)
	}

	query, err := invoke[types.QueryResponse](t, engine, tools["ecs_query"], &types.QueryRequest{Components: []string{name[spot](), name[label]()}})
	if err != nil {
		t.Fatalf("ecs_query: %v", err)
	}
	if query.Total != 121 || !query.Truncated || len(query.Entities) != 50 {
		t.Errorf("ecs_query answers total %d, truncated %v, %d Entities; want 121, true, 50 by default", query.Total, query.Truncated, len(query.Entities))
	}
}

// Both refusal paths reach an Agent as mcp.Unavailable, an ordinary tool error
// it reads and acts on, never as a successful empty answer.
func TestTheToolsRefuseAsUnavailable(t *testing.T) {
	engine, tools := startAgentRig(t)
	executioner := engine.Executioner()
	gone := spawn(executioner, make([]plainSet, 3)...)[2]
	executioner.ExecuteCommand[despawnCmd]([]ecs.Entity{gone})

	_, err := invoke[types.QueryResponse](t, engine, tools["ecs_query"], &types.QueryRequest{Components: []string{"ecs.spott"}})
	var unavailable mcp.Unavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("a misspelt Component answered %v, want mcp.Unavailable", err)
	}
	census, err := invoke[types.CensusResponse](t, engine, tools["ecs_world"], &types.CensusRequest{})
	if err != nil {
		t.Fatalf("ecs_world: %v", err)
	}
	for _, component := range census.Components {
		if !strings.Contains(unavailable.Reason, component.Name) {
			t.Errorf("the refusal %q does not list %s", unavailable.Reason, component.Name)
		}
	}

	_, err = invoke[types.EntityResponse](t, engine, tools["ecs_entity"], &types.EntityRequest{Entity: gone.String()})
	if !errors.As(err, &unavailable) {
		t.Fatalf("a despawned Entity answered %v, want mcp.Unavailable", err)
	}
	if !strings.Contains(unavailable.Reason, "not alive") {
		t.Errorf("the refusal %q does not say the Entity is not alive", unavailable.Reason)
	}
}

// The Provider costs a game nobody is debugging nothing: it subscribes nothing,
// and the kernel binds it to the collected Port as ecs.McpProvider from ecs.
//
// The drainer is the one subscription the ecs plugin owns, and it is not the
// Provider's: an Agent attached to a running game adds no node to any frame.
func TestTheProviderIsBoundAndSubscribesNothing(t *testing.T) {
	engine, _ := startAgentRig(t)
	description := engine.Describe()
	for _, subscription := range description.Subscriptions {
		if subscription.Owner == ecs.Name && subscription.Type != reflect.TypeFor[ecs.DrainOnUpdate]() {
			t.Errorf("the ecs plugin subscribes %s", kernel.TypeName(subscription.Type))
		}
	}
	want := kernel.AdapterDescription{Type: reflect.TypeFor[ecs.McpProvider](), Plugin: ecs.Name}
	for _, port := range description.Ports {
		if port.Type != reflect.TypeFor[mcp.ProviderPort]() {
			continue
		}
		if !slices.Contains(port.Adapters, want) {
			t.Fatalf("mcp.ProviderPort binds %v, want %v among them", port.Adapters, want)
		}
		return
	}
	t.Fatalf("the architecture has no mcp.ProviderPort: %v", description.Ports)
}

// The prompt text is reproduced word for word in the spec, so it is reviewed
// as prompt text: every description and every field's jsonschema prose appears
// there, compared with line breaks and blockquote marks folded away.
func TestTheSpecReproducesThePromptTextWordForWord(t *testing.T) {
	source, err := os.ReadFile("../docs/specs/mcp.md")
	if err != nil {
		t.Fatalf("reading the spec: %v", err)
	}
	fold := func(text string) string {
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		for i, line := range lines {
			lines[i] = strings.TrimPrefix(strings.TrimPrefix(line, ">"), " ")
		}
		return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	}
	spec := fold(string(source))
	for _, one := range (provider{}).Capabilities() {
		if !strings.Contains(spec, fold(one.Description())) {
			t.Errorf("the spec does not reproduce the %s description word for word", one.Name())
		}
	}
	for _, payload := range []reflect.Type{
		reflect.TypeFor[types.EntityRequest](), reflect.TypeFor[types.QueryRequest](),
		reflect.TypeFor[types.QueryResponse](), reflect.TypeFor[types.ComponentValue](),
	} {
		for field := range payload.Fields() {
			if prose := field.Tag.Get("jsonschema"); prose != "" && !strings.Contains(spec, fold(prose)) {
				t.Errorf("the spec does not reproduce %s.%s's jsonschema prose word for word", payload.Name(), field.Name)
			}
		}
	}
}
