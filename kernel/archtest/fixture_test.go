package archtest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixtureModule is a miniature cog: one of every kind, wired the way the rules
// allow, so that the clean tree has no violations and each failure test adds
// exactly one. slots/s is a Slot, extensions/e the Extension filling it, and
// bundles/n a Bundle collecting a Port; bundles/b stands for any other plugin.
var fixtureModule = map[string]string{
	"go.mod": "module fixture.test/cog\n\ngo 1.27\n",
	"kernel/kernel.go": `package kernel

type PluginName string

type Registrar struct{}

type Plugin interface {
	Name() PluginName
	Dependencies() []PluginName
	Register(registrar *Registrar, config any) error
}

type RequiredPort[I any] = func(requiredPort) I

type CollectedPort[I any] = func(collectedPort) I

type Adapter[P any] = func(adapterOf) P

type (
	requiredPort  struct{}
	collectedPort struct{}
	adapterOf     struct{}
)

func (r *Registrar) ProvideAdapter[A any](adapter any) {}
`,
	"libs/l/l.go": `package l

import _ "fixture.test/cog/kernel"

type Point struct{ X, Y int }
`,
	"slots/s/doc.go": `// Package s is a Slot: it cannot work until an Adapter fills its MainLoop.
package s
`,
	"slots/s/id.go": `package s

import "fixture.test/cog/kernel"

const Name kernel.PluginName = "s"
`,
	"slots/s/ports.go": `package s

import "fixture.test/cog/kernel"

type MainLoop interface{ Run() error }

type MainLoopPort kernel.RequiredPort[MainLoop]
`,
	"slots/s/types.go": `package s

import "fixture.test/cog/slots/s/internal/types"

type Queue = types.Queue
`,
	"slots/s/utils.go": `package s

import (
	"fixture.test/cog/libs/l"
	"fixture.test/cog/slots/s/internal/types"
)

func NewQueue(size int, at l.Point) Queue { return types.NewQueue(size, at) }
`,
	"slots/s/internal/types/queue.go": `package types

import "fixture.test/cog/libs/l"

type Queue struct{ items []l.Point }

func NewQueue(size int, at l.Point) Queue { return Queue{items: make([]l.Point, 0, size)} }
`,
	"slots/s/internal/plugin.go": `package internal

import (
	"fixture.test/cog/kernel"
	"fixture.test/cog/slots/s"
	_ "fixture.test/cog/slots/s/internal/types"
)

type plugin struct{}

func New() kernel.Plugin { return plugin{} }

func (plugin) Name() kernel.PluginName                              { return s.Name }
func (plugin) Dependencies() []kernel.PluginName                    { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error { return nil }
`,
	"slots/s/splugin/splugin.go": `package splugin

import (
	"fixture.test/cog/kernel"
	"fixture.test/cog/slots/s/internal"
)

func New() kernel.Plugin { return internal.New() }
`,
	"bundles/n/id.go": `package n

import "fixture.test/cog/kernel"

const Name kernel.PluginName = "n"
`,
	"bundles/n/ports.go": `package n

import (
	"fixture.test/cog/kernel"
	_ "fixture.test/cog/slots/s"
)

type Provider interface{ Tools() []string }

type ProviderPort kernel.CollectedPort[Provider]
`,
	"bundles/n/internal/plugin.go": `package internal

import (
	_ "fixture.test/cog/bundles/b"
	"fixture.test/cog/bundles/n"
	"fixture.test/cog/kernel"
)

type plugin struct{}

func New() kernel.Plugin { return plugin{} }

func (plugin) Name() kernel.PluginName                              { return n.Name }
func (plugin) Dependencies() []kernel.PluginName                    { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error { return nil }
`,
	"bundles/n/nplugin/nplugin.go": `package nplugin

import (
	"fixture.test/cog/bundles/n/internal"
	"fixture.test/cog/kernel"
)

func New() kernel.Plugin { return internal.New() }
`,
	"bundles/n/n_test.go": `package n_test

import (
	_ "fixture.test/cog/bundles/n/internal"
	_ "fixture.test/cog/bundles/n/nplugin"
	_ "fixture.test/cog/extensions/e/eplugin"
)
`,
	"extensions/e/doc.go": `// Package e is an Extension: it fills s's MainLoop and contributes to n.
package e
`,
	"extensions/e/id.go": `package e

import "fixture.test/cog/kernel"

const Name kernel.PluginName = "e"
`,
	"extensions/e/config.go": `package e

type Config struct{ Title string }

func (c Config) WithTitle(title string) Config { c.Title = title; return c }
`,
	"extensions/e/adapters.go": `package e

import (
	"fixture.test/cog/bundles/n"
	"fixture.test/cog/kernel"
	"fixture.test/cog/slots/s"
)

type SMainLoop kernel.Adapter[s.MainLoopPort]

type NProvider kernel.Adapter[n.ProviderPort]
`,
	"extensions/e/err.go": `package e

type ErrNoWindow struct{}

func (ErrNoWindow) Error() string { return "no window" }
`,
	"extensions/e/internal/plugin.go": `package internal

import (
	"fixture.test/cog/bundles/n"
	"fixture.test/cog/extensions/e"
	"fixture.test/cog/kernel"
	"fixture.test/cog/slots/s"
)

type plugin struct{}

type mainLoop struct{}

func (mainLoop) Run() error { return nil }

type provider struct{}

func (provider) Tools() []string { return nil }

func New() kernel.Plugin { return plugin{} }

func (plugin) Name() kernel.PluginName           { return e.Name }
func (plugin) Dependencies() []kernel.PluginName { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error {
	registrar.ProvideAdapter[e.SMainLoop](s.MainLoop(mainLoop{}))
	registrar.ProvideAdapter[e.NProvider](n.Provider(provider{}))
	return nil
}
`,
	"extensions/e/eplugin/eplugin.go": `package eplugin

import (
	"fixture.test/cog/extensions/e/internal"
	"fixture.test/cog/kernel"
)

func New() kernel.Plugin { return internal.New() }
`,
	"bundles/b/id.go": `package b

import "fixture.test/cog/kernel"

const Name kernel.PluginName = "b"
`,
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		addFile(t, root, name, body)
	}
	return root
}

func addFile(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureViolations checks a fixture tree after confirming that the clean tree
// passes, so a failure test can only be failing on the file it added.
func fixtureViolations(t *testing.T, name, body string) []violation {
	t.Helper()
	return fixtureViolationsWith(t, map[string]string{name: body})
}

// fixtureViolationsWith is fixtureViolations adding or replacing several files.
func fixtureViolationsWith(t *testing.T, files map[string]string) []violation {
	t.Helper()
	requireGo(t)
	root := writeFixture(t, fixtureModule)
	if clean := check(t, root); len(clean) != 0 {
		t.Fatalf("the clean fixture has violations:\n%s", joinViolations(clean))
	}
	for name, body := range files {
		addFile(t, root, name, body)
	}
	return check(t, root)
}

func TestTiers_TheCleanFixturePasses(t *testing.T) {
	requireGo(t)
	root := writeFixture(t, fixtureModule)
	if violations := check(t, root); len(violations) != 0 {
		t.Fatalf("the clean fixture has violations:\n%s", joinViolations(violations))
	}
}

func requireOne(t *testing.T, violations []violation, want ...string) {
	t.Helper()
	if len(violations) != 1 {
		t.Fatalf("want one violation, got %d:\n%s", len(violations), joinViolations(violations))
	}
	message := violations[0].String()
	for _, part := range want {
		if !strings.Contains(message, part) {
			t.Errorf("violation %q does not name %q", message, part)
		}
	}
}

// Only a type with all three kernel.Plugin methods breaks the Plugin rule: a
// root type that is handed a *kernel.Registrar without being a Plugin passes.
func TestTiers_ARootTypeWithoutEveryPluginMethodPasses(t *testing.T) {
	violations := fixtureViolations(t, "bundles/n/types.go", `package n

import "fixture.test/cog/kernel"

type Planner struct{}

func (Planner) Name() kernel.PluginName                              { return Name }
func (*Planner) Register(registrar *kernel.Registrar, config any) error { return nil }
`)
	if len(violations) != 0 {
		t.Fatalf("a root type with only some Plugin methods has violations:\n%s", joinViolations(violations))
	}
}

// Tests of the kernel and of Libraries get no exception: they may not reach a
// plugin's constructor package either.
func TestTiers_AKernelOrLibraryImportingAPluginFails(t *testing.T) {
	for name, test := range map[string]struct{ file, body, key, rule string }{
		"the kernel's test": {
			file: "kernel/reach_test.go",
			body: "package kernel_test\n\nimport _ \"fixture.test/cog/bundles/n/nplugin\"\n",
			key:  "kernel/reach_test.go:3: kernel imports bundles/n/nplugin",
			rule: ruleKernel,
		},
		"a Library's test": {
			file: "libs/l/reach_test.go",
			body: "package l\n\nimport _ \"fixture.test/cog/bundles/n/nplugin\"\n",
			key:  "libs/l/reach_test.go:3: libs/l imports bundles/n/nplugin",
			rule: ruleLib,
		},
	} {
		t.Run(name, func(t *testing.T) {
			requireOne(t, fixtureViolations(t, test.file, test.body), test.key, test.rule)
		})
	}
}

// retiredLayoutPlugin is a plugin in the layout declaration roots replaced: a
// root holding code under a free file name, a qimpl package constructing the
// plugin, and an internal/ package the root imports.
var retiredLayoutPlugin = map[string]string{
	"bundles/q/q.go": `package q

import _ "fixture.test/cog/bundles/q/internal/shared"

const Name = "q"
`,
	"bundles/q/internal/shared/shared.go": `package shared
`,
	"bundles/q/qimpl/qimpl.go": `package qimpl

import (
	"fixture.test/cog/bundles/q"
	"fixture.test/cog/kernel"
)

type plugin struct{}

func New() kernel.Plugin { return plugin{} }

func DefaultConfig() Config { return Config{} }

type Config struct{}

func (plugin) Name() kernel.PluginName                              { return q.Name }
func (plugin) Dependencies() []kernel.PluginName                    { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error { return nil }
`,
}

func TestTiers_APluginInTheRetiredLayoutFails(t *testing.T) {
	violations := fixtureViolationsWith(t, retiredLayoutPlugin)
	want := []string{
		"bundles/q/q.go: bundles/q holds q.go: " + ruleRootFiles,
		"bundles/q/q.go:3: bundles/q imports bundles/q/internal/shared: " + ruleRootImports,
		"bundles/q/qimpl: bundles/q/qimpl matches no tier: " + ruleNoTier,
	}
	var got []string
	for _, v := range violations {
		got = append(got, v.String())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("want violations\n\t%s\ngot\n%s", strings.Join(want, "\n\t"), joinViolations(violations))
	}
}

func TestTiers_ABundleRootFileOutsideTheAllowlistFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/n/helpers.go", "package n\n")
	requireOne(t, violations, "bundles/n/helpers.go: bundles/n holds helpers.go", ruleRootFiles)
}

// commands.go is on a Slot's and a Bundle's allowlist, and not on an
// Extension's.
func TestTiers_AnExtensionRootFileOutsideItsAllowlistFails(t *testing.T) {
	violations := fixtureViolations(t, "extensions/e/commands.go", "package e\n")
	requireOne(t, violations, "extensions/e/commands.go: extensions/e holds commands.go", ruleExtensionFiles)
}

// Tests, non-Go files and a docs/ directory are outside the allowlist.
func TestTiers_ASlotRootHoldingEveryAllowedFilePasses(t *testing.T) {
	files := map[string]string{
		"slots/s/README.md":        "# s\n",
		"slots/s/docs/specs/s.md":  "# s\n",
		"slots/s/queue_test.go":    "package s\n",
		"slots/s/external_test.go": "package s_test\n",
	}
	for _, name := range []string{"commands.go", "events.go", "resources.go", "adapters.go", "config.go", "err.go"} {
		files["slots/s/"+name] = "package s\n"
	}
	if violations := fixtureViolationsWith(t, files); len(violations) != 0 {
		t.Fatalf("a Slot root holding only allowed files has violations:\n%s", joinViolations(violations))
	}
}

func TestTiers_ARootImportingItsOwnInternalFails(t *testing.T) {
	violations := fixtureViolationsWith(t, map[string]string{
		"bundles/n/internal/helper/helper.go": "package helper\n",
		"bundles/n/doc.go": `// Package n is a Bundle.
package n

import _ "fixture.test/cog/bundles/n/internal/helper"
`,
	})
	requireOne(t, violations, "bundles/n/doc.go:4", "bundles/n imports bundles/n/internal/helper", ruleRootImports)
}

// bundles/n's root imports no internal/types, so the edge back is not a cycle.
func TestTiers_InternalTypesImportingItsOwnRootFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/n/internal/types/reach.go", `package types

import _ "fixture.test/cog/bundles/n"
`)
	requireOne(t, violations, "bundles/n/internal/types/reach.go:3", "bundles/n/internal/types imports bundles/n", ruleTypes)
}

func TestTiers_AConstructorImportingARootFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/n/nplugin/reach.go", `package nplugin

import _ "fixture.test/cog/bundles/n"
`)
	requireOne(t, violations, "bundles/n/nplugin/reach.go:3", "bundles/n/nplugin imports bundles/n", ruleConstructor)
}

func TestTiers_InternalImportingAConstructorFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/n/internal/reach.go", `package internal

import _ "fixture.test/cog/slots/s/splugin"
`)
	requireOne(t, violations, "bundles/n/internal/reach.go:3", "bundles/n/internal imports slots/s/splugin", ruleReach)
}
