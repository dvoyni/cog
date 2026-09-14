package archtest

import "testing"

// The fixtures for what a root may declare, as opposed to what it may import.

func TestTiers_AnExportedRootFunctionOutsideUtilsFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/n/types.go", `package n

func Tools() []string { return nil }
`)
	requireOne(t, violations, "bundles/n/types.go:3", "bundles/n declares Tools outside utils.go", ruleForwarder)
}

func TestTiers_ANonForwarderInUtilsFails(t *testing.T) {
	header := `package s

import (
	"fixture.test/cog/libs/l"
	"fixture.test/cog/slots/s/internal/types"
)

`
	for name, body := range map[string]string{
		"a changed argument":  `func NewQueue(size int, at l.Point) Queue { return types.NewQueue(size*2, at) }`,
		"reordered arguments": `func NewQueue(at l.Point, size int) Queue { return types.NewQueue(size, at) }`,
		"a second statement": `func NewQueue(size int, at l.Point) Queue {
	_ = l.Point{}
	return types.NewQueue(size, at)
}`,
		"a call outside internal/types": `func NewQueue(size int, at l.Point) Queue { return newQueue(size, at) }

func newQueue(size int, at l.Point) Queue { return types.NewQueue(size, at) }`,
		"a conversion": `func NewQueue(size int, at l.Point) Queue { return types.Queue(types.NewQueue(size, at)) }`,
		"a method": `func NewQueue(size int, at l.Point) Queue { return types.NewQueue(size, at) }

type Size int

func (s Size) Queue(at l.Point) Queue { return types.NewQueue(int(s), at) }`,
	} {
		t.Run(name, func(t *testing.T) {
			violations := fixtureViolations(t, "slots/s/utils.go", header+body+"\n")
			if len(violations) == 0 {
				t.Fatal("want a violation, got none")
			}
			for _, v := range violations {
				if v.rule != ruleForwarder || v.file != "slots/s/utils.go" {
					t.Errorf("unexpected violation %s", v)
				}
			}
		})
	}
}

// A forwarder may be generic, variadic or return nothing, as long as it passes
// its type parameters and parameters through unchanged.
func TestTiers_GenericVariadicAndVoidForwardersPass(t *testing.T) {
	violations := fixtureViolationsWith(t, fixtureUnmoved, map[string]string{
		"slots/s/internal/types/more.go": `package types

type Box[T any] struct{ value T }

func Get[T any](key string) Box[T] { return Box[T]{} }

func Join(parts ...string) string { return "" }

func Reset(queue *Queue) {}
`,
		"slots/s/types.go": `package s

import "fixture.test/cog/slots/s/internal/types"

type Queue = types.Queue

type Box[T any] = types.Box[T]
`,
		"slots/s/utils.go": `package s

import (
	"fixture.test/cog/libs/l"
	"fixture.test/cog/slots/s/internal/types"
)

func NewQueue(size int, at l.Point) Queue { return types.NewQueue(size, at) }

func Get[T any](key string) Box[T] { return types.Get[T](key) }

func Join(parts ...string) string { return types.Join(parts...) }

func Reset(queue *Queue) { types.Reset(queue) }
`,
	})
	if len(violations) != 0 {
		t.Fatalf("pure forwarders have violations:\n%s", joinViolations(violations))
	}
}

// bundles/b stands for another plugin: slots/s cannot import bundles/n, which
// imports it.
func TestTiers_ASlotForwarderNamingAnotherPluginsTypeFails(t *testing.T) {
	files := map[string]string{
		"bundles/b/thing.go": "package b\n\ntype Thing struct{}\n",
		"slots/s/internal/types/thing.go": `package types

import "fixture.test/cog/bundles/b"

func Wrap(thing b.Thing) []b.Thing { return nil }
`,
		"slots/s/utils.go": `package s

import (
	"fixture.test/cog/bundles/b"
	"fixture.test/cog/libs/l"
	"fixture.test/cog/slots/s/internal/types"
)

func NewQueue(size int, at l.Point) Queue { return types.NewQueue(size, at) }

func Wrap(thing b.Thing) []b.Thing { return types.Wrap(thing) }
`,
	}
	violations := fixtureViolationsWith(t, fixtureUnmoved, files)
	requireOne(t, violations,
		"slots/s/utils.go:11",
		"slots/s forwards Wrap, whose signature names fixture.test/cog/bundles/b.Thing",
		ruleSlotForwarder,
	)
}

// A Bundle's forwarders may name any root's types.
func TestTiers_ABundleForwarderNamingAnotherPluginsTypePasses(t *testing.T) {
	violations := fixtureViolationsWith(t, fixtureUnmoved, map[string]string{
		"bundles/b/thing.go": "package b\n\ntype Thing struct{}\n",
		"bundles/n/internal/types/thing.go": `package types

import "fixture.test/cog/bundles/b"

func Wrap(thing b.Thing) []b.Thing { return nil }
`,
		"bundles/n/utils.go": `package n

import (
	"fixture.test/cog/bundles/b"
	"fixture.test/cog/bundles/n/internal/types"
)

func Wrap(thing b.Thing) []b.Thing { return types.Wrap(thing) }
`,
	})
	if len(violations) != 0 {
		t.Fatalf("a Bundle forwarder naming another plugin's type has violations:\n%s", joinViolations(violations))
	}
}

// Its own types, aliases of its internal/types included, type parameters,
// predeclared types, the standard library, Libraries and the kernel are all a
// Slot's forwarder may name.
func TestTiers_ASlotForwarderNamingItsOwnStandardLibraryAndKernelTypesPasses(t *testing.T) {
	violations := fixtureViolationsWith(t, fixtureUnmoved, map[string]string{
		"kernel/write.go": "package kernel\n\ntype Write[T any] struct{ value T }\n",
		"slots/s/internal/types/open.go": `package types

import (
	"context"

	"fixture.test/cog/kernel"
	"fixture.test/cog/libs/l"
)

func Open[T any](ctx context.Context, at [2]l.Point, handle kernel.Write[Queue], values map[string]T, done func(error)) (*Queue, <-chan struct{ At l.Point }) {
	return nil, nil
}
`,
		"slots/s/utils.go": `package s

import (
	"context"

	"fixture.test/cog/kernel"
	"fixture.test/cog/libs/l"
	"fixture.test/cog/slots/s/internal/types"
)

func NewQueue(size int, at l.Point) Queue { return types.NewQueue(size, at) }

func Open[T any](ctx context.Context, at [2]l.Point, handle kernel.Write[Queue], values map[string]T, done func(error)) (*Queue, <-chan struct{ At l.Point }) {
	return types.Open[T](ctx, at, handle, values, done)
}
`,
	})
	if len(violations) != 0 {
		t.Fatalf("a Slot forwarder naming only allowed types has violations:\n%s", joinViolations(violations))
	}
}

func TestTiers_AConstructorExportingMoreThanNewFails(t *testing.T) {
	header := `package nplugin

import (
	"fixture.test/cog/bundles/n/internal"
	"fixture.test/cog/kernel"
)

`
	for name, test := range map[string]struct{ body, export string }{
		"a Config": {
			body:   "func New() kernel.Plugin { return internal.New() }\n\ntype Config struct{}\n",
			export: "Config",
		},
		"a DefaultConfig": {
			body:   "func New() kernel.Plugin { return internal.New() }\n\nfunc DefaultConfig() struct{} { return struct{}{} }\n",
			export: "DefaultConfig",
		},
		"an error": {
			body:   "func New() kernel.Plugin { return internal.New() }\n\ntype ErrInvalidConfig struct{}\n",
			export: "ErrInvalidConfig",
		},
		"a New taking an argument": {
			body:   "func New(config any) kernel.Plugin { return internal.New() }\n",
			export: "New",
		},
	} {
		t.Run(name, func(t *testing.T) {
			violations := fixtureViolations(t, "bundles/n/nplugin/nplugin.go", header+test.body)
			requireOne(t, violations, "bundles/n/nplugin/nplugin.go", "bundles/n/nplugin exports "+test.export, ruleConstructorExports)
		})
	}
}

func TestTiers_ProvidingAnUndeclaredAdapterFails(t *testing.T) {
	for name, test := range map[string]struct{ file, body, key string }{
		"a type of its own root outside adapters.go": {
			file: "extensions/e/internal/extra.go",
			body: `package internal

import (
	"fixture.test/cog/extensions/e"
	"fixture.test/cog/kernel"
)

func provideMore(registrar *kernel.Registrar) {
	registrar.ProvideAdapter[e.ErrNoWindow](nil)
}
`,
			key: "extensions/e/internal/extra.go:9: extensions/e provides e.ErrNoWindow, which extensions/e/adapters.go does not declare",
		},
		"another plugin's Adapter": {
			file: "bundles/n/internal/extra.go",
			body: `package internal

import (
	"fixture.test/cog/extensions/e"
	"fixture.test/cog/kernel"
)

func provideMore(registrar *kernel.Registrar) {
	registrar.ProvideAdapter[e.SDriver](nil)
}
`,
			key: "bundles/n/internal/extra.go:9: bundles/n provides e.SDriver, which bundles/n/adapters.go does not declare",
		},
	} {
		t.Run(name, func(t *testing.T) {
			violations := fixtureViolations(t, test.file, test.body)
			requireOne(t, violations, test.key, ruleUndeclaredAdapter)
		})
	}
}

func TestTiers_DeclaringAnAdapterNeverProvidedFails(t *testing.T) {
	violations := fixtureViolations(t, "extensions/e/adapters.go", `package e

import (
	"fixture.test/cog/bundles/n"
	"fixture.test/cog/kernel"
	"fixture.test/cog/slots/s"
)

type SDriver kernel.Adapter[s.DriverPort]

type NProvider kernel.Adapter[n.ProviderPort]

type NSecondProvider kernel.Adapter[n.ProviderPort]
`)
	requireOne(t, violations,
		"extensions/e/adapters.go:13",
		"extensions/e declares NSecondProvider and never provides it",
		ruleUnprovidedAdapter,
	)
}

// A ProvideAdapter the go tool builds only for another platform still counts,
// and so does one spelled with every type argument.
func TestTiers_AnAdapterProvidedOnlyForAnotherPlatformPasses(t *testing.T) {
	violations := fixtureViolationsWith(t, fixtureUnmoved, map[string]string{
		"extensions/e/internal/plugin.go": `package internal

import (
	"fixture.test/cog/bundles/n"
	"fixture.test/cog/extensions/e"
	"fixture.test/cog/kernel"
)

type plugin struct{}

type provider struct{}

func (provider) Tools() []string { return nil }

func New() kernel.Plugin { return plugin{} }

func (plugin) Name() kernel.PluginName           { return e.Name }
func (plugin) Dependencies() []kernel.PluginName { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error {
	registrar.ProvideAdapter[e.NProvider](n.Provider(provider{}))
	provideDriver(registrar)
	return nil
}
`,
		"extensions/e/internal/driver_js.go": `//go:build js

package internal

import (
	adapters "fixture.test/cog/extensions/e"
	"fixture.test/cog/kernel"
	"fixture.test/cog/slots/s"
)

type driver struct{}

func (driver) Run() error { return nil }

func provideDriver(registrar *kernel.Registrar) {
	registrar.ProvideAdapter[adapters.SDriver, s.DriverPort](s.Driver(driver{}))
}
`,
		"extensions/e/internal/driver_other.go": `//go:build !js

package internal

import "fixture.test/cog/kernel"

func provideDriver(registrar *kernel.Registrar) {}
`,
	})
	if len(violations) != 0 {
		t.Fatalf("an Adapter provided only under js has violations:\n%s", joinViolations(violations))
	}
}

// A collected Port does not make a Slot.
func TestTiers_ASlotRootDeclaringNoRequiredPortFails(t *testing.T) {
	violations := fixtureViolationsWith(t, fixtureUnmoved, map[string]string{
		"slots/t/id.go": `package t

import "fixture.test/cog/kernel"

const Name kernel.PluginName = "t"
`,
		"slots/t/ports.go": `package t

import "fixture.test/cog/kernel"

type Listener interface{ Hear() }

type ListenerPort kernel.CollectedPort[Listener]
`,
	})
	requireOne(t, violations, "slots/t: slots/t declares no required Port", ruleSlotPort)
}

func TestTiers_ABundleRootDeclaringARequiredPortFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/n/ports.go", `package n

import (
	"fixture.test/cog/kernel"
	_ "fixture.test/cog/slots/s"
)

type Provider interface{ Tools() []string }

type ProviderPort kernel.CollectedPort[Provider]

type HostPort kernel.RequiredPort[Provider]
`)
	requireOne(t, violations, "bundles/n/ports.go:12", "bundles/n declares the required Port HostPort", ruleNoRequiredPort)
}

func TestTiers_AnExtensionRootWithAPIFails(t *testing.T) {
	for name, test := range map[string]struct{ file, body, declares string }{
		"a command": {
			file:     "extensions/e/id.go",
			body:     "package e\n\nimport \"fixture.test/cog/kernel\"\n\nconst Name kernel.PluginName = \"e\"\n\ntype StartCmd func()\n",
			declares: "StartCmd",
		},
		"a constant": {
			file:     "extensions/e/id.go",
			body:     "package e\n\nimport \"fixture.test/cog/kernel\"\n\nconst Name kernel.PluginName = \"e\"\n\nconst Version = 2\n",
			declares: "Version",
		},
		"a variable": {
			file:     "extensions/e/config.go",
			body:     "package e\n\ntype Config struct{ Title string }\n\nvar Default = Config{}\n",
			declares: "Default",
		},
		"an interface": {
			file:     "extensions/e/err.go",
			body:     "package e\n\ntype ErrNoWindow struct{}\n\nfunc (ErrNoWindow) Error() string { return \"no window\" }\n\ntype Window interface{ Open() }\n",
			declares: "Window",
		},
	} {
		t.Run(name, func(t *testing.T) {
			violations := fixtureViolations(t, test.file, test.body)
			requireOne(t, violations, test.file, "extensions/e declares "+test.declares, ruleExtensionAPI)
		})
	}
}

// Contributing to a collected Port alone does not make an Extension.
func TestTiers_AnExtensionFillingNoRequiredPortFails(t *testing.T) {
	violations := fixtureViolationsWith(t, fixtureUnmoved, map[string]string{
		"extensions/e/adapters.go": `package e

import (
	"fixture.test/cog/bundles/n"
	"fixture.test/cog/kernel"
)

type NProvider kernel.Adapter[n.ProviderPort]
`,
		"extensions/e/internal/plugin.go": `package internal

import (
	"fixture.test/cog/bundles/n"
	"fixture.test/cog/extensions/e"
	"fixture.test/cog/kernel"
)

type plugin struct{}

type provider struct{}

func (provider) Tools() []string { return nil }

func New() kernel.Plugin { return plugin{} }

func (plugin) Name() kernel.PluginName           { return e.Name }
func (plugin) Dependencies() []kernel.PluginName { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error {
	registrar.ProvideAdapter[e.NProvider](n.Provider(provider{}))
	return nil
}
`,
	})
	requireOne(t, violations, "extensions/e: extensions/e declares no Adapter for a required Port", ruleExtensionAdapter)
}

// A root in the declaration-root shape is held to the Plugin rule too.
func TestTiers_AnExtensionRootDeclaringAPluginFails(t *testing.T) {
	violations := fixtureViolations(t, "extensions/e/err.go", `package e

import "fixture.test/cog/kernel"

type ErrNoWindow struct{}

func (ErrNoWindow) Error() string                                       { return "no window" }
func (ErrNoWindow) Name() kernel.PluginName                              { return Name }
func (ErrNoWindow) Dependencies() []kernel.PluginName                    { return nil }
func (ErrNoWindow) Register(registrar *kernel.Registrar, config any) error { return nil }
`)
	requireOne(t, violations, "extensions/e/err.go:5", "extensions/e declares ErrNoWindow", rulePlugin)
}
