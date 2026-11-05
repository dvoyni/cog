package kernel

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// greeter is a socket's interface; greeting implements it.
type greeter interface{ Greet() string }

type greeting string

func (g greeting) Greet() string { return string(g) }

// binder is a plugin binding value to the socket of T.
func binder[T any](name PluginName, value T) hookPlugin {
	return hookPlugin{name: name, init: func(c *Configurator) { c.BindSocket[T](value) }}
}

func TestBindingsResolveWhateverThePluginOrder(t *testing.T) {
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	var one ExclusiveSocket[greeter]
	var many Socket[int]
	var gotOne string
	var gotMany []int
	consumer := hookPlugin{
		name: "consumer",
		init: func(c *Configurator) {
			one = c.DeclareExclusiveSocket[greeter](false)
			many = c.DeclareSocket[int](false)
		},
		start: func(*Kernel) {
			gotOne = one.Get().Greet()
			gotMany = many.GetAll()
		},
	}
	_, err := s.Spawn(nil, 1,
		binder[int]("first", 1),
		consumer,
		binder[greeter]("greeter", greeting("hello")),
		binder[int]("second", 2),
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotOne != "hello" {
		t.Errorf("the exclusive socket answers %q, want hello", gotOne)
	}
	if !slices.Equal(gotMany, []int{1, 2}) {
		t.Errorf("the socket answers %v, want [1 2] in plugin order", gotMany)
	}
}

func TestOptionalSocketsMayStayUnbound(t *testing.T) {
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	var one ExclusiveSocket[greeter]
	var many Socket[int]
	started := false
	_, err := s.Spawn(nil, 1, hookPlugin{
		name: "consumer",
		init: func(c *Configurator) {
			one = c.DeclareExclusiveSocket[greeter](true)
			many = c.DeclareSocket[int](true)
		},
		start: func(*Kernel) {
			started = true
			if one.Get() != nil {
				t.Errorf("an unbound optional exclusive socket answers %v, want nil", one.Get())
			}
			if len(many.GetAll()) != 0 {
				t.Errorf("an unbound optional socket answers %v", many.GetAll())
			}
		},
	})
	if err != nil || !started {
		t.Fatalf("Spawn answered %v, started %v", err, started)
	}
}

func TestGetBeforeTheBindingsResolvePanics(t *testing.T) {
	for _, test := range []struct {
		name string
		get  func(c *Configurator)
	}{
		{"exclusive", func(c *Configurator) { c.DeclareExclusiveSocket[greeter](true).Get() }},
		{"multiple", func(c *Configurator) { c.DeclareSocket[int](true).GetAll() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			expectPanicSaying(t, "read in Init", func() {
				s := NewSupervisor(nil)
				t.Cleanup(s.Quit)
				s.Spawn(nil, 1, hookPlugin{name: "consumer", init: test.get})
			})
		})
	}
}

func TestSocketCompositionErrorsPanic(t *testing.T) {
	declare := func(name PluginName, optional bool) hookPlugin {
		return hookPlugin{name: name, init: func(c *Configurator) {
			c.DeclareExclusiveSocket[greeter](optional)
			c.DeclareSocket[int](true)
		}}
	}
	for _, test := range []struct {
		name    string
		plugins []Plugin
		want    []string
	}{
		{"declared twice", []Plugin{declare("a", true), declare("b", true)},
			[]string{"kernel.greeter", "declared twice", `"a"`, `"b"`}},
		{"two bindings of an exclusive socket", []Plugin{
			declare("consumer", false), binder[greeter]("one", greeting("1")), binder[greeter]("two", greeting("2")),
		}, []string{"kernel.greeter", "2 bindings", `"one"`, `"two"`}},
		{"an exclusive socket unbound", []Plugin{declare("consumer", false)},
			[]string{"kernel.greeter", `"consumer"`, "no binding"}},
		{"a socket unbound", []Plugin{hookPlugin{name: "consumer", init: func(c *Configurator) {
			c.DeclareSocket[int](false)
		}}}, []string{"int", `"consumer"`, "no binding"}},
		{"a binding nobody declares", []Plugin{declare("consumer", true), binder[string]("stray", "x")},
			[]string{`"stray"`, "string", "no plugin declares"}},
		{"a nil binding", []Plugin{declare("consumer", true), binder[greeter]("nil", nil)},
			[]string{`"nil"`, "nil", "kernel.greeter"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			expectPanicSaying(t, strings.Join(test.want, "\x00"), func() {
				s := NewSupervisor(nil)
				t.Cleanup(s.Quit)
				s.Spawn(nil, 1, test.plugins...)
			})
		})
	}
}

// expectPanicSaying runs f and fails unless it panics with a message
// containing every part of want, the parts split by NUL.
func expectPanicSaying(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		message := fmt.Sprint(recover())
		for _, part := range strings.Split(want, "\x00") {
			if !strings.Contains(message, part) {
				t.Fatalf("panicked with %q, want it to say %q", message, part)
			}
		}
	}()
	f()
}
