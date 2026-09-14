package kernel

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// testBackend is the interface a required Port declares.
type testBackend interface{ Label() string }

// testCapability is the interface a collected Port declares.
type testCapability interface{ Label() string }

// testBackendPort needs exactly one Adapter; testCapabilityPort takes any number.
type (
	testBackendPort    RequiredPort[testBackend]
	testCapabilityPort CollectedPort[testCapability]
)

// testGPU and testSoftware both fill testBackendPort; testOffer contributes to
// testCapabilityPort.
type (
	testGPU      Adapter[testBackendPort]
	testSoftware Adapter[testBackendPort]
	testOffer    Adapter[testCapabilityPort]
)

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

// A required Adapter is bound at composition and read from Start. The Port's
// plugin is listed before its contributor and neither declares the other, so the
// binding does not depend on registration order and adds no dependency edge.
func TestPorts_RequiredAdapterIsBoundAndReadInStart(t *testing.T) {
	var got string
	port := &testPlugin{name: "port"}
	port.register = func(r *Registrar) error {
		backend := r.RequireAdapter[testBackendPort]()
		port.start = func(Executioner) error {
			got = backend.Get().Label()
			return nil
		}
		return nil
	}
	adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
		r.ProvideAdapter[testGPU](testBackend(testLabel("gpu")))
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
		r.RequireAdapter[testBackendPort]()
		return nil
	}}

	_, err := composeForTest(port)

	var missing ErrMissingAdapter
	if !errors.As(err, &missing) {
		t.Fatalf("composition error = %v, want ErrMissingAdapter", err)
	}
	if want := (ErrMissingAdapter{Plugin: "port", Port: reflect.TypeFor[testBackendPort]()}); missing != want {
		t.Fatalf("missing = %+v, want %+v", missing, want)
	}
	if want := `plugin "port" requires an adapter for kernel.testBackendPort, but no plugin provides one`; err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

func TestPorts_DuplicateRequiredAdaptersFailComposition(t *testing.T) {
	port := testPlugin{name: "port", register: func(r *Registrar) error {
		r.RequireAdapter[testBackendPort]()
		return nil
	}}
	first := testPlugin{name: "first", register: func(r *Registrar) error {
		r.ProvideAdapter[testGPU](testBackend(testLabel("first")))
		return nil
	}}
	second := testPlugin{name: "second", register: func(r *Registrar) error {
		r.ProvideAdapter[testSoftware](testBackend(testLabel("second")))
		return nil
	}}

	_, err := composeForTest(port, first, second)

	var duplicate ErrDuplicateAdapter
	if !errors.As(err, &duplicate) {
		t.Fatalf("composition error = %v, want ErrDuplicateAdapter", err)
	}
	want := ErrDuplicateAdapter{Plugin: "port", Port: reflect.TypeFor[testBackendPort](), Adapters: []AdapterDescription{
		{Type: reflect.TypeFor[testGPU](), Plugin: "first"},
		{Type: reflect.TypeFor[testSoftware](), Plugin: "second"},
	}}
	if !reflect.DeepEqual(duplicate, want) {
		t.Fatalf("duplicate = %+v, want %+v", duplicate, want)
	}
	wantMessage := `plugin "port" requires exactly one adapter for kernel.testBackendPort, ` +
		`but several are provided: [kernel.testGPU (first), kernel.testSoftware (second)]`
	if err.Error() != wantMessage {
		t.Fatalf("message = %q, want %q", err.Error(), wantMessage)
	}
}

// A nil Adapter is refused when it is provided, so a Port requiring it fails
// composition with a named error instead of a binding panic.
func TestPorts_NilAdapterForRequiredPortFailsComposition(t *testing.T) {
	port := testPlugin{name: "port", register: func(r *Registrar) error {
		r.RequireAdapter[testBackendPort]()
		return nil
	}}
	adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
		r.ProvideAdapter[testGPU](nil)
		return nil
	}}

	_, err := composeForTest(port, adapter)

	var nilAdapter ErrNilAdapter
	if !errors.As(err, &nilAdapter) {
		t.Fatalf("composition error = %v, want ErrNilAdapter", err)
	}
	if want := (ErrNilAdapter{Plugin: "adapter", Adapter: reflect.TypeFor[testGPU]()}); nilAdapter != want {
		t.Fatalf("nil adapter = %+v, want %+v", nilAdapter, want)
	}
	if want := `plugin "adapter" provides a nil kernel.testGPU`; !strings.Contains(err.Error(), want) {
		t.Fatalf("composition error = %v, want it to contain %q", err, want)
	}
}

// A nil Adapter for a collected Port is refused the same way. The refusal is the
// only failure: the collected Port's other contributions bind cleanly.
func TestPorts_NilAdapterForCollectedPortFailsComposition(t *testing.T) {
	broker := testPlugin{name: "broker", register: func(r *Registrar) error {
		r.CollectAdapters[testCapabilityPort]()
		return nil
	}}
	contributor := func(name PluginName, adapter testCapability) testPlugin {
		return testPlugin{name: name, register: func(r *Registrar) error {
			r.ProvideAdapter[testOffer](adapter)
			return nil
		}}
	}

	_, err := composeForTest(broker, contributor("first", testLabel("first")),
		contributor("empty", nil), contributor("second", testLabel("second")))

	var nilAdapter ErrNilAdapter
	if !errors.As(err, &nilAdapter) {
		t.Fatalf("composition error = %v, want ErrNilAdapter", err)
	}
	want := ErrNilAdapter{Plugin: "empty", Adapter: reflect.TypeFor[testOffer]()}
	if nilAdapter != want {
		t.Fatalf("nil adapter = %+v, want %+v", nilAdapter, want)
	}
	if err.Error() != want.Error() {
		t.Fatalf("composition error = %v, want only %v", err, want)
	}
}

// A nil Adapter fails composition even when no plugin declares its Port, so the
// mistake does not lie dormant until one is added.
func TestPorts_NilAdapterNobodyConsumesFailsComposition(t *testing.T) {
	adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
		r.ProvideAdapter[testOffer](nil)
		return nil
	}}

	_, err := composeForTest(adapter)

	var nilAdapter ErrNilAdapter
	if !errors.As(err, &nilAdapter) {
		t.Fatalf("composition error = %v, want ErrNilAdapter", err)
	}
	if want := (ErrNilAdapter{Plugin: "adapter", Adapter: reflect.TypeFor[testOffer]()}); nilAdapter != want {
		t.Fatalf("nil adapter = %+v, want %+v", nilAdapter, want)
	}
}

// nilReceiverLabel implements testBackend on a nil pointer.
type nilReceiverLabel struct{}

func (l *nilReceiverLabel) Label() string {
	if l == nil {
		return "nil receiver"
	}
	return "value"
}

// A typed nil is a valid interface value, not a nil Adapter: it binds and
// reaches the plugin requiring the Port.
func TestPorts_TypedNilPointerAdapterIsAccepted(t *testing.T) {
	var got string
	port := &testPlugin{name: "port"}
	port.register = func(r *Registrar) error {
		backend := r.RequireAdapter[testBackendPort]()
		port.start = func(Executioner) error {
			got = backend.Get().Label()
			return nil
		}
		return nil
	}
	adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
		r.ProvideAdapter[testGPU](testBackend((*nilReceiverLabel)(nil)))
		return nil
	}}

	startEngine(t, port, adapter)

	if got != "nil receiver" {
		t.Fatalf("Start read adapter %q, want the typed nil's %q", got, "nil receiver")
	}
}

// Collected Adapters arrive in plugin order, which is dependency order rather
// than the order the caller listed plugins in, each with its contributor's name.
func TestPorts_CollectedAdaptersComeInPluginOrderWithContributors(t *testing.T) {
	var got []ContributedAdapter[testCapability]
	broker := &testPlugin{name: "broker"}
	broker.register = func(r *Registrar) error {
		capabilities := r.CollectAdapters[testCapabilityPort]()
		r.ProvideAdapter[testOffer](testCapability(testLabel("broker-own")))
		broker.start = func(Executioner) error {
			got = capabilities.Get()
			return nil
		}
		return nil
	}
	contributor := func(name PluginName, deps ...PluginName) testPlugin {
		return testPlugin{name: name, deps: deps, register: func(r *Registrar) error {
			r.ProvideAdapter[testOffer](testCapability(testLabel(name)))
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
		capabilities := r.CollectAdapters[testCapabilityPort]()
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

// An Adapter for a Port no plugin requires or collects binds to nothing and fails
// nothing: a Bundle may contribute to a Port the engine lacks.
func TestPorts_UnconsumedAdapterIsNotAnError(t *testing.T) {
	adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
		r.ProvideAdapter[testOffer](testCapability(testLabel("orphan")))
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

// Two Ports on the same interface are distinct: an Adapter binds only to the
// Port its type names.
func TestPorts_AnAdapterBindsOnlyToItsOwnPort(t *testing.T) {
	type otherBackendPort RequiredPort[testBackend]
	port := testPlugin{name: "port", register: func(r *Registrar) error {
		r.RequireAdapter[otherBackendPort]()
		return nil
	}}
	adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
		r.ProvideAdapter[testGPU](testBackend(testLabel("gpu")))
		return nil
	}}

	_, err := composeForTest(port, adapter)

	var missing ErrMissingAdapter
	if !errors.As(err, &missing) || missing.Port != reflect.TypeFor[otherBackendPort]() {
		t.Fatalf("composition error = %v, want ErrMissingAdapter for otherBackendPort", err)
	}
}

func TestPorts_GetBeforeFinalizationPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(r *Registrar)
	}{
		{name: "required", read: func(r *Registrar) { r.RequireAdapter[testBackendPort]().Get() }},
		{name: "collected", read: func(r *Registrar) { r.CollectAdapters[testCapabilityPort]().Get() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := testPlugin{name: "port", register: func(r *Registrar) error {
				r.ProvideAdapter[testGPU](testBackend(testLabel("gpu")))
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

// A Port built on a type that is not an interface is refused by each of the
// three declarations.
func TestPorts_NonInterfacePortPanics(t *testing.T) {
	type labelPort RequiredPort[testLabel]
	type labelsPort CollectedPort[*testLabel]
	type labelAdapter Adapter[labelPort]
	for _, tc := range []struct {
		name    string
		declare func(r *Registrar)
	}{
		{name: "require", declare: func(r *Registrar) { r.RequireAdapter[labelPort]() }},
		{name: "collect", declare: func(r *Registrar) { r.CollectAdapters[labelsPort]() }},
		{name: "provide", declare: func(r *Registrar) { r.ProvideAdapter[labelAdapter](testLabel("concrete")) }},
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

func TestPorts_OnePluginDeclaringAPortTwiceFailsComposition(t *testing.T) {
	for _, tc := range []struct {
		name    string
		port    reflect.Type
		declare func(r *Registrar)
	}{
		{name: "require twice", port: reflect.TypeFor[testBackendPort](), declare: func(r *Registrar) {
			r.RequireAdapter[testBackendPort]()
			r.RequireAdapter[testBackendPort]()
		}},
		{name: "collect twice", port: reflect.TypeFor[testCapabilityPort](), declare: func(r *Registrar) {
			r.CollectAdapters[testCapabilityPort]()
			r.CollectAdapters[testCapabilityPort]()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := testPlugin{name: "port", register: func(r *Registrar) error {
				tc.declare(r)
				return nil
			}}
			adapter := testPlugin{name: "adapter", register: func(r *Registrar) error {
				r.ProvideAdapter[testGPU](testBackend(testLabel("gpu")))
				return nil
			}}

			_, err := composeForTest(port, adapter)

			var duplicate ErrDuplicateRegistration
			if !errors.As(err, &duplicate) {
				t.Fatalf("composition error = %v, want ErrDuplicateRegistration", err)
			}
			want := ErrDuplicateRegistration{Kind: "port declaration", Type: tc.port, Owner: "port", Existing: "port"}
			if duplicate != want {
				t.Fatalf("duplicate = %+v, want %+v", duplicate, want)
			}
		})
	}
}

// Describe lists each declaration by its Port type, with the Adapter types bound
// to it, and Dump renders the same as a ports section.
func TestPorts_DescribeNamesPortAndAdapterTypes(t *testing.T) {
	gfx := testPlugin{name: "gfx", register: func(r *Registrar) error {
		r.RequireAdapter[testBackendPort]()
		return nil
	}}
	broker := testPlugin{name: "broker", register: func(r *Registrar) error {
		r.CollectAdapters[testCapabilityPort]()
		return nil
	}}
	wgpu := testPlugin{name: "wgpu", deps: []PluginName{"gfx"}, register: func(r *Registrar) error {
		r.ProvideAdapter[testGPU](testBackend(testLabel("gpu")))
		r.ProvideAdapter[testOffer](testCapability(testLabel("wgpu")))
		return nil
	}}
	input := testPlugin{name: "input", register: func(r *Registrar) error {
		r.ProvideAdapter[testOffer](testCapability(testLabel("input")))
		return nil
	}}

	e, err := composeForTest(wgpu, broker, gfx, input)
	if err != nil {
		t.Fatalf("composition error = %v", err)
	}

	want := []PortDescription{
		{
			Type: reflect.TypeFor[testBackendPort](), Interface: reflect.TypeFor[testBackend](), Owner: "gfx",
			Adapters: []AdapterDescription{{Type: reflect.TypeFor[testGPU](), Plugin: "wgpu"}},
		},
		{
			Type: reflect.TypeFor[testCapabilityPort](), Interface: reflect.TypeFor[testCapability](), Owner: "broker",
			Collects: true, Adapters: []AdapterDescription{
				{Type: reflect.TypeFor[testOffer](), Plugin: "wgpu"},
				{Type: reflect.TypeFor[testOffer](), Plugin: "input"},
			},
		},
	}
	if got := e.Describe().Ports; !reflect.DeepEqual(got, want) {
		t.Fatalf("ports = %+v, want %+v", got, want)
	}
	wantDump := "ports:\n" +
		"  kernel.testBackendPort (gfx) requires [kernel.testGPU (wgpu)]\n" +
		"  kernel.testCapabilityPort (broker) collects [kernel.testOffer (wgpu), kernel.testOffer (input)]\n" +
		"commands:\n"
	if dump := Dump(e); !strings.Contains(dump, wantDump) {
		t.Fatalf("dump missing ports section %q:\n%s", wantDump, dump)
	}
}
