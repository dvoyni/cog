package kernel

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// testBackend is the interface a required-adapter Port declares.
type testBackend interface{ Label() string }

// testCapability is the interface a collecting Port declares.
type testCapability interface{ Label() string }

type testLabel string

func (l testLabel) Label() string { return string(l) }

// composeForTest runs WithPlugins and returns every error composition reported.
func composeForTest(plugins ...Plugin) (*Engine, error) {
	var handled []error
	e := New(nil).Handler(func(err error) bool {
		handled = append(handled, err)
		return true
	}).WithPlugins(plugins...)
	return e, errors.Join(handled...)
}

// A required Adapter is bound at composition and read from Start. The Port is
// listed before its contributor and neither declares the other, so the binding
// does not depend on registration order and adds no dependency edge.
func TestPorts_RequiredAdapterIsBoundAndReadInStart(t *testing.T) {
	var got string
	port := &testPlugin{name: "port"}
	port.register = func(r *Registrar) error {
		backend := r.RequireAdapter[testBackend]()
		port.start = func(Executioner) error {
			got = backend.Get().Label()
			return nil
		}
		return nil
	}
	adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
		r.ProvideAdapter[testBackend](testLabel("gpu"))
		return nil
	}}

	e := startEngine(t, port, adapter)

	if got != "gpu" {
		t.Fatalf("Start read adapter %q, want %q", got, "gpu")
	}
	names := make([]PluginName, 0, 2)
	for _, plugin := range e.Describe().Plugins {
		names = append(names, plugin.Name)
		if len(plugin.Dependencies) != 0 {
			t.Fatalf("plugin %s gained dependencies %v from an adapter binding", plugin.Name, plugin.Dependencies)
		}
	}
	if !slices.Equal(names, []PluginName{"port", "adapter"}) {
		t.Fatalf("plugin order = %v, want the caller's [port adapter]", names)
	}
}

func TestPorts_MissingRequiredAdapterFailsComposition(t *testing.T) {
	port := testPlugin{name: "port", register: func(r *Registrar) error {
		r.RequireAdapter[testBackend]()
		return nil
	}}

	_, err := composeForTest(port)

	var missing ErrMissingAdapter
	if !errors.As(err, &missing) {
		t.Fatalf("composition error = %v, want ErrMissingAdapter", err)
	}
	if want := (ErrMissingAdapter{Port: "port", Interface: reflect.TypeFor[testBackend]()}); missing != want {
		t.Fatalf("missing = %+v, want %+v", missing, want)
	}
}

func TestPorts_DuplicateRequiredAdaptersFailComposition(t *testing.T) {
	port := testPlugin{name: "port", register: func(r *Registrar) error {
		r.RequireAdapter[testBackend]()
		return nil
	}}
	provider := func(name PluginName) testPlugin {
		return testPlugin{name: name, register: func(r *Registrar) error {
			r.ProvideAdapter[testBackend](testLabel(name))
			return nil
		}}
	}

	_, err := composeForTest(port, provider("first"), provider("second"))

	var duplicate ErrDuplicateAdapter
	if !errors.As(err, &duplicate) {
		t.Fatalf("composition error = %v, want ErrDuplicateAdapter", err)
	}
	if duplicate.Port != "port" || duplicate.Interface != reflect.TypeFor[testBackend]() ||
		!slices.Equal(duplicate.Contributors, []PluginName{"first", "second"}) {
		t.Fatalf("duplicate = %+v", duplicate)
	}
}

// Collected Adapters arrive in plugin order, which is dependency order rather
// than the order the caller listed plugins in, each with its contributor's name.
func TestPorts_CollectedAdaptersComeInPluginOrderWithContributors(t *testing.T) {
	var got []ContributedAdapter[testCapability]
	broker := &testPlugin{name: "broker"}
	broker.register = func(r *Registrar) error {
		capabilities := r.CollectAdapters[testCapability]()
		r.ProvideAdapter[testCapability](testLabel("broker-own"))
		broker.start = func(Executioner) error {
			got = capabilities.Get()
			return nil
		}
		return nil
	}
	contributor := func(name PluginName, deps ...PluginName) testPlugin {
		return testPlugin{name: name, deps: deps, register: func(r *Registrar) error {
			r.ProvideAdapter[testCapability](testLabel(name))
			return nil
		}}
	}

	startEngine(t, contributor("later", "earlier"), broker, contributor("earlier"))

	var rendered []string
	for _, contribution := range got {
		rendered = append(rendered, fmt.Sprintf("%s=%s", contribution.Plugin, contribution.Adapter.Label()))
	}
	want := []string{"broker=broker-own", "earlier=earlier", "later=later"}
	if !slices.Equal(rendered, want) {
		t.Fatalf("collected = %v, want %v", rendered, want)
	}
}

func TestPorts_CollectingZeroAdaptersIsValid(t *testing.T) {
	called := false
	var got []ContributedAdapter[testCapability]
	broker := &testPlugin{name: "broker"}
	broker.register = func(r *Registrar) error {
		capabilities := r.CollectAdapters[testCapability]()
		broker.start = func(Executioner) error {
			called = true
			got = capabilities.Get()
			return nil
		}
		return nil
	}

	startEngine(t, broker)

	if !called || len(got) != 0 {
		t.Fatalf("Start called %v, collected %v, want called with none", called, got)
	}
}

// An Adapter for an interface no plugin requires or collects binds to nothing
// and fails nothing: a Bundle may contribute to a Port the engine lacks.
func TestPorts_UnconsumedAdapterIsNotAnError(t *testing.T) {
	adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
		r.ProvideAdapter[testCapability](testLabel("orphan"))
		return nil
	}}

	e, err := composeForTest(adapter)

	if err != nil {
		t.Fatalf("composition error = %v, want none", err)
	}
	if ports := e.Describe().Ports; len(ports) != 0 {
		t.Fatalf("ports = %+v, want none for an unconsumed adapter", ports)
	}
}

func TestPorts_GetBeforeFinalizationPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(r *Registrar)
	}{
		{name: "required", read: func(r *Registrar) { r.RequireAdapter[testBackend]().Get() }},
		{name: "collected", read: func(r *Registrar) { r.CollectAdapters[testCapability]().Get() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := testPlugin{name: "port", register: func(r *Registrar) error {
				r.ProvideAdapter[testBackend](testLabel("gpu"))
				tc.read(r)
				return nil
			}}

			_, err := composeForTest(port)

			var panicked ErrPluginPanic
			if !errors.As(err, &panicked) || panicked.Plugin != "port" || panicked.Boundary != "Register" {
				t.Fatalf("composition error = %v, want a Register panic from port", err)
			}
			if message := fmt.Sprint(panicked.Recovered); !strings.Contains(message, "before composition bound it") {
				t.Fatalf("panic = %q, want it to say the handle is not bound yet", message)
			}
		})
	}
}

func TestPorts_NonInterfaceTypeArgumentPanics(t *testing.T) {
	for _, tc := range []struct {
		name    string
		declare func(r *Registrar)
	}{
		{name: "require", declare: func(r *Registrar) { r.RequireAdapter[testLabel]() }},
		{name: "collect", declare: func(r *Registrar) { r.CollectAdapters[*testLabel]() }},
		{name: "provide", declare: func(r *Registrar) { r.ProvideAdapter(testLabel("inferred")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testPlugin{name: "p", register: func(r *Registrar) error {
				tc.declare(r)
				return nil
			}}

			_, err := composeForTest(p)

			var panicked ErrPluginPanic
			if !errors.As(err, &panicked) || panicked.Plugin != "p" || panicked.Boundary != "Register" {
				t.Fatalf("composition error = %v, want a Register panic from p", err)
			}
			if message := fmt.Sprint(panicked.Recovered); !strings.Contains(message, "not an interface") {
				t.Fatalf("panic = %q, want it to say the type is not an interface", message)
			}
		})
	}
}

func TestPorts_OnePluginDeclaringAnInterfaceTwiceFailsComposition(t *testing.T) {
	for _, tc := range []struct {
		name    string
		declare func(r *Registrar)
	}{
		{name: "require and collect", declare: func(r *Registrar) {
			r.RequireAdapter[testBackend]()
			r.CollectAdapters[testBackend]()
		}},
		{name: "require twice", declare: func(r *Registrar) {
			r.RequireAdapter[testBackend]()
			r.RequireAdapter[testBackend]()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := testPlugin{name: "port", register: func(r *Registrar) error {
				tc.declare(r)
				return nil
			}}
			adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
				r.ProvideAdapter[testBackend](testLabel("gpu"))
				return nil
			}}

			_, err := composeForTest(port, adapter)

			var duplicate ErrDuplicateRegistration
			if !errors.As(err, &duplicate) {
				t.Fatalf("composition error = %v, want ErrDuplicateRegistration", err)
			}
			want := ErrDuplicateRegistration{
				Kind: "adapter declaration", Type: reflect.TypeFor[testBackend](), Owner: "port", Existing: "port",
			}
			if duplicate != want {
				t.Fatalf("duplicate = %+v, want %+v", duplicate, want)
			}
		})
	}
}

// Describe lists each declaration with its Port and contributors, and Dump
// renders the same as a ports section.
func TestPorts_DescribeListsPortsAndContributors(t *testing.T) {
	gfx := testPlugin{name: "gfx", register: func(r *Registrar) error {
		r.RequireAdapter[testBackend]()
		return nil
	}}
	broker := testPlugin{name: "broker", register: func(r *Registrar) error {
		r.CollectAdapters[testCapability]()
		return nil
	}}
	wgpu := testPlugin{name: "wgpu", deps: []PluginName{"gfx"}, register: func(r *Registrar) error {
		r.ProvideAdapter[testBackend](testLabel("gpu"))
		r.ProvideAdapter[testCapability](testLabel("wgpu"))
		return nil
	}}
	input := testPlugin{name: "input", register: func(r *Registrar) error {
		r.ProvideAdapter[testCapability](testLabel("input"))
		return nil
	}}

	e, err := composeForTest(wgpu, broker, gfx, input)
	if err != nil {
		t.Fatalf("composition error = %v", err)
	}

	want := []PortDescription{
		{Interface: reflect.TypeFor[testBackend](), Port: "gfx", Contributors: []PluginName{"wgpu"}},
		{Interface: reflect.TypeFor[testCapability](), Port: "broker", Collects: true, Contributors: []PluginName{"wgpu", "input"}},
	}
	if got := e.Describe().Ports; !reflect.DeepEqual(got, want) {
		t.Fatalf("ports = %+v, want %+v", got, want)
	}
	wantDump := "ports:\n" +
		"  kernel.testBackend (gfx) requires [wgpu]\n" +
		"  kernel.testCapability (broker) collects [wgpu input]\n" +
		"commands:\n"
	if dump := Dump(e); !strings.Contains(dump, wantDump) {
		t.Fatalf("dump missing ports section %q:\n%s", wantDump, dump)
	}
}
