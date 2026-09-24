package internal_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecs/internal"
	"github.com/dvoyni/cog/kernel"
)

// Twin is internal.Twin's twin: another type kernel.TypeName renders as ecs.Twin.
type Twin struct{ There int }

type twinSet struct {
	Here  internal.Twin
	There Twin
}

type (
	twinSpawnCmd  kernel.Command[struct{}, struct{}]
	twinQueryCmd  kernel.Command[internal.QueryRequest, internal.QueryResponse]
	twinCensusCmd kernel.Command[internal.CensusRequest, internal.CensusResponse]
)

// twins registers both Twins and reaches the read by Component name through the
// same factories the ecs plugin registers its read Commands with, whose types
// are unexported in a package this test cannot see into.
type twins struct{}

func (twins) Name() kernel.PluginName { return "twins" }

func (twins) Dependencies() []kernel.PluginName { return []kernel.PluginName{internal.Name} }

func (twins) Register(registrar *kernel.Registrar, _ any) error {
	internal.RegisterComponent[internal.Twin](registrar, 4)
	internal.RegisterComponent[Twin](registrar, 4)
	registrar.HandleCommand[twinSpawnCmd](internal.ToExecute[struct{}, struct{}](registrar, func(sp *internal.Spawn[twinSet]) {
		sp.New(twinSet{Here: internal.Twin{Here: 1}, There: Twin{There: 2}})
	}))
	registrar.HandleCommand[twinQueryCmd](internal.QueryCommand)
	registrar.HandleCommand[twinCensusCmd](internal.CensusCommand)
	return nil
}

// TestAnAmbiguousNameIsRefusedAndItsQualifiedFormsResolve: a name two types
// render alike resolves to neither, the refusal lists both package-qualified
// forms, and each of those resolves to its own type.
func TestAnAmbiguousNameIsRefusedAndItsQualifiedFormsResolve(t *testing.T) {
	here, there := reflect.TypeFor[internal.Twin](), reflect.TypeFor[Twin]()
	if kernel.TypeName(here) != "ecs.Twin" || kernel.TypeName(there) != "ecs.Twin" {
		t.Fatalf("the twins render as %s and %s, want both ecs.Twin", kernel.TypeName(here), kernel.TypeName(there))
	}
	engine := kernel.New(nil).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(ecsplugin.New(), twins{})
	stopped := make(chan struct{})
	t.Cleanup(func() {
		engine.Quit()
		<-stopped
	})
	go func() {
		defer close(stopped)
		engine.Run()
	}()
	<-engine.Ready()
	executioner := engine.Executioner()
	executioner.ExecuteCommand[twinSpawnCmd](struct{}{})

	hereForm := here.PkgPath() + ".Twin"
	thereForm := there.PkgPath() + ".Twin"
	ambiguous := executioner.ExecuteCommand[twinQueryCmd](internal.QueryRequest{Components: []string{"ecs.Twin"}})
	if !strings.Contains(ambiguous.Refusal, hereForm) || !strings.Contains(ambiguous.Refusal, thereForm) {
		t.Fatalf("ecs.Twin answered %+v, want a refusal naming %s and %s", ambiguous, hereForm, thereForm)
	}
	for form, want := range map[string]string{hereForm: `"Here":1`, thereForm: `"There":2`} {
		answer := executioner.ExecuteCommand[twinQueryCmd](internal.QueryRequest{Components: []string{form}})
		if answer.Refusal != "" || answer.Total != 1 {
			t.Fatalf("%s answered %+v, want its one Entity", form, answer)
		}
		value := answer.Entities[0].Components[0]
		if value.Name != "ecs.Twin" || !strings.Contains(stringOf(t, value.Value), want) {
			t.Errorf("%s answered %+v, want the value holding %s", form, value, want)
		}
	}
	census := executioner.ExecuteCommand[twinCensusCmd](internal.CensusRequest{})
	var twinsListed int
	for _, component := range census.Components {
		if component.Name == "ecs.Twin" {
			twinsListed++
		}
	}
	if twinsListed != 2 {
		t.Errorf("the census lists ecs.Twin %d times, want once per type", twinsListed)
	}
}

func stringOf(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("the value %#v does not encode: %v", value, err)
	}
	return string(encoded)
}
