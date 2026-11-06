package kernel

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// calledPlugin records every lifecycle call it gets in calls, as "name.Step",
// and fails each step named in failing. It names dependencies as its own.
type calledPlugin struct {
	name         PluginName
	dependencies []PluginName
	calls        *[]string
	failing      string
}

func (p calledPlugin) Name() PluginName { return p.name }

func (p calledPlugin) Dependencies() []PluginName { return p.dependencies }

func (p calledPlugin) Init(configurator *Configurator, config any) error {
	return p.call("Init")
}

func (p calledPlugin) Start(kernel *Kernel) error { return p.call("Start") }

func (p calledPlugin) Stop(kernel *Kernel) error { return p.call("Stop") }

func (p calledPlugin) Destroy() error { return p.call("Destroy") }

func (p calledPlugin) call(step string) error {
	*p.calls = append(*p.calls, string(p.name)+"."+step)
	if strings.Contains(p.failing, step) {
		return errors.New(step + " failed")
	}
	return nil
}

// newTestLifecycle answers the lifecycle of a partial kernel over plugins,
// logging into handler.
func newTestLifecycle(handler LogHandler, plugins ...Plugin) *lifecycle {
	k := newPartialKernel(nil, nil, 0)
	k.logger = &Logger{handler: handler}
	k.lifecycle.plugins = plugins
	return &k.lifecycle
}

func TestFailedInitDestroysTheInitializedInReverse(t *testing.T) {
	var calls []string
	l := newTestLifecycle(&errorLogHandler{},
		calledPlugin{name: "a", calls: &calls},
		calledPlugin{name: "b", calls: &calls},
		calledPlugin{name: "c", calls: &calls, failing: "Init"},
		calledPlugin{name: "d", calls: &calls},
	)
	err := l.init()
	if err == nil || !strings.Contains(err.Error(), `plugin "c" failed to init`) {
		t.Fatalf("init answered %v, want c's failure by name", err)
	}
	if want := []string{"a.Init", "b.Init", "c.Init", "b.Destroy", "a.Destroy"}; !slices.Equal(calls, want) {
		t.Fatalf("calls are %v, want %v", calls, want)
	}
}

func TestFailedStartStopsTheStartedInReverse(t *testing.T) {
	var calls []string
	l := newTestLifecycle(&errorLogHandler{},
		calledPlugin{name: "a", calls: &calls},
		calledPlugin{name: "b", calls: &calls},
		calledPlugin{name: "c", calls: &calls, failing: "Start"},
		calledPlugin{name: "d", calls: &calls},
	)
	err := l.start()
	if err == nil || !strings.Contains(err.Error(), `plugin "c" failed to start`) {
		t.Fatalf("start answered %v, want c's failure by name", err)
	}
	if want := []string{"a.Start", "b.Start", "c.Start", "b.Stop", "a.Stop"}; !slices.Equal(calls, want) {
		t.Fatalf("calls are %v, want %v", calls, want)
	}
}

func TestStopAndDestroyRunInReverseLoggingByName(t *testing.T) {
	var calls []string
	handler := &errorLogHandler{}
	l := newTestLifecycle(handler,
		calledPlugin{name: "a", calls: &calls},
		calledPlugin{name: "b", calls: &calls, failing: "Stop,Destroy"},
		calledPlugin{name: "c", calls: &calls},
	)
	l.stop()
	l.destroy()
	if want := []string{"c.Stop", "b.Stop", "a.Stop", "c.Destroy", "b.Destroy", "a.Destroy"}; !slices.Equal(calls, want) {
		t.Fatalf("calls are %v, want %v", calls, want)
	}
	logged := handler.logged()
	if len(logged) != 2 ||
		!strings.Contains(logged[0].Error(), `plugin "b" failed to stop`) ||
		!strings.Contains(logged[1].Error(), `plugin "b" failed to destroy`) {
		t.Fatalf("logged %v, want b's stop and destroy failures by name", logged)
	}
}

func TestTwoPluginsOfOneNamePanicBeforeAnyInit(t *testing.T) {
	var calls []string
	l := newTestLifecycle(&errorLogHandler{},
		calledPlugin{name: "a", calls: &calls},
		calledPlugin{name: "a", calls: &calls},
	)
	expectPanic(t, "two plugins of one name", func() { l.init() })
	if len(calls) != 0 {
		t.Fatalf("calls are %v, want none before the panic", calls)
	}
}
