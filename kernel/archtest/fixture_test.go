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
// exactly one. slots/s, bundles/n and extensions/e have the declaration-root
// shape; the rest keep the shape from before it and are on fixtureUnmoved.
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
	"slots/s/doc.go": `// Package s is a Slot: it cannot work until an Adapter fills its Driver.
package s
`,
	"slots/s/id.go": `package s

import "fixture.test/cog/kernel"

const Name kernel.PluginName = "s"
`,
	"slots/s/ports.go": `package s

import "fixture.test/cog/kernel"

type Driver interface{ Run() error }

type DriverPort kernel.RequiredPort[Driver]
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
	_ "fixture.test/cog/extensions/p/v"
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
	_ "fixture.test/cog/bundles/b/bimpl"
	_ "fixture.test/cog/bundles/n/nplugin"
	_ "fixture.test/cog/extensions/e/eplugin"
	_ "fixture.test/cog/extensions/w"
)
`,
	"extensions/e/doc.go": `// Package e is an Extension: it fills s's Driver and contributes to n.
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

type SDriver kernel.Adapter[s.DriverPort]

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

type driver struct{}

func (driver) Run() error { return nil }

type provider struct{}

func (provider) Tools() []string { return nil }

func New() kernel.Plugin { return plugin{} }

func (plugin) Name() kernel.PluginName           { return e.Name }
func (plugin) Dependencies() []kernel.PluginName { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error {
	registrar.ProvideAdapter[e.SDriver](s.Driver(driver{}))
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
	"slots/o/o.go": `package o

import (
	_ "fixture.test/cog/bundles/b"
	_ "fixture.test/cog/extensions/p"
	_ "fixture.test/cog/kernel"
	_ "fixture.test/cog/libs/l"
)
`,
	"bundles/a/a.go": `package a

import (
	_ "fixture.test/cog/bundles/a/internal/shared"
	_ "fixture.test/cog/bundles/b"
	_ "fixture.test/cog/extensions/p"
	_ "fixture.test/cog/slots/o"
	_ "fixture.test/cog/slots/s"
)

const Name = "a"
`,
	"bundles/a/a_test.go": `package a

import (
	_ "fixture.test/cog/bundles/b/bimpl"
	_ "fixture.test/cog/extensions/w"
)
`,
	"bundles/a/internal/shared/shared.go": `package shared

import _ "fixture.test/cog/bundles/b"
`,
	"bundles/a/aimpl/aimpl.go": `package aimpl

import (
	"fixture.test/cog/bundles/a"
	_ "fixture.test/cog/bundles/a/internal/shared"
	"fixture.test/cog/kernel"
)

type plugin struct{}

func New() kernel.Plugin { return plugin{} }

func (plugin) Name() kernel.PluginName                              { return a.Name }
func (plugin) Dependencies() []kernel.PluginName                    { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error { return nil }
`,
	"bundles/b/b.go": `package b

import _ "fixture.test/cog/kernel"
`,
	"bundles/b/bimpl/bimpl.go": `package bimpl

import _ "fixture.test/cog/bundles/b"
`,
	"extensions/p/p.go": `package p

import _ "fixture.test/cog/bundles/b"
`,
	"extensions/p/pimpl/pimpl.go": `package pimpl

import (
	_ "fixture.test/cog/extensions/p"
	_ "fixture.test/cog/extensions/p/v"
)
`,
	"extensions/p/v/v.go": `package v

import _ "fixture.test/cog/libs/l"
`,
	"extensions/w/w.go": `package w

import (
	_ "fixture.test/cog/bundles/b"
	_ "fixture.test/cog/extensions/p"
	_ "fixture.test/cog/extensions/p/v"
)
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

// fixtureUnmoved is the fixture's migration list: the plugins that keep the
// shape from before declaration roots.
var fixtureUnmoved = []string{"bundles/a", "bundles/b", "extensions/p", "extensions/w", "slots/o"}

// fixtureViolations checks a fixture tree after confirming that the clean tree
// passes, so a failure test can only be failing on the file it added.
func fixtureViolations(t *testing.T, name, body string) []violation {
	t.Helper()
	return fixtureViolationsWith(t, fixtureUnmoved, map[string]string{name: body})
}

// fixtureViolationsWith is fixtureViolations adding or replacing several files,
// checked with a migration list of its own.
func fixtureViolationsWith(t *testing.T, unmoved []string, files map[string]string) []violation {
	t.Helper()
	requireGo(t)
	root := writeFixture(t, fixtureModule)
	if clean := check(t, root, fixtureUnmoved); len(clean) != 0 {
		t.Fatalf("the clean fixture has violations:\n%s", joinViolations(clean))
	}
	for name, body := range files {
		addFile(t, root, name, body)
	}
	return check(t, root, unmoved)
}

func TestTiers_TheCleanFixturePasses(t *testing.T) {
	requireGo(t)
	root := writeFixture(t, fixtureModule)
	if violations := check(t, root, fixtureUnmoved); len(violations) != 0 {
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

func TestTiers_ABundleRootImportingAnotherBundlesImplFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/a/reach.go", `package a

import _ "fixture.test/cog/bundles/b/bimpl"
`)
	requireOne(t, violations,
		"bundles/a/reach.go:3",
		"bundles/a imports bundles/b/bimpl",
		ruleImpl,
	)
}

func TestTiers_AContractRootDeclaringAPluginFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/a/plugin.go", `package a

import "fixture.test/cog/kernel"

type plugin struct{}

func (plugin) Name() kernel.PluginName                              { return Name }
func (plugin) Dependencies() []kernel.PluginName                    { return nil }
func (*plugin) Register(registrar *kernel.Registrar, config any) error { return nil }
`)
	requireOne(t, violations,
		"bundles/a/plugin.go:5",
		"bundles/a declares plugin",
		rulePlugin,
	)
}

func TestTiers_AnImplExportingAPluginTypeFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/b/bimpl/plugin.go", `package bimpl

import "fixture.test/cog/kernel"

type Plugin struct{}

func (Plugin) Name() kernel.PluginName                              { return "b" }
func (Plugin) Dependencies() []kernel.PluginName                    { return nil }
func (Plugin) Register(registrar *kernel.Registrar, config any) error { return nil }
`)
	requireOne(t, violations,
		"bundles/b/bimpl/plugin.go:5",
		"bundles/b/bimpl exports Plugin",
		ruleImplExports,
	)
}

func TestTiers_AnImplWhoseNewReturnsAConcretePointerFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/b/bimpl/plugin.go", `package bimpl

import "fixture.test/cog/kernel"

type plugin struct{}

func New() *plugin { return &plugin{} }

func (*plugin) Name() kernel.PluginName                              { return "b" }
func (*plugin) Dependencies() []kernel.PluginName                    { return nil }
func (*plugin) Register(registrar *kernel.Registrar, config any) error { return nil }
`)
	requireOne(t, violations,
		"bundles/b/bimpl/plugin.go:7",
		"bundles/b/bimpl exports New",
		ruleImplExports,
	)
}

// A Port's vocabulary is what its Adapters implement against, so it may not
// reach back into the Port's recording half: importing its own root is the edge
// that would erode the split.
func TestTiers_AVocabularyImportingItsPortRootFails(t *testing.T) {
	violations := fixtureViolations(t, "extensions/p/v/reach.go", `package v

import _ "fixture.test/cog/extensions/p"
`)
	requireOne(t, violations,
		"extensions/p/v/reach.go:3",
		"extensions/p/v imports extensions/p",
		ruleVocabulary,
	)
}

// A Bundle names the Port's IDs, formats and descriptors from the vocabulary
// directly, the way it would from the Port's root.
func TestTiers_ABundleImportingAPortVocabularyPasses(t *testing.T) {
	violations := fixtureViolations(t, "bundles/a/vocabulary.go", `package a

import _ "fixture.test/cog/extensions/p/v"
`)
	if len(violations) != 0 {
		t.Fatalf("a Bundle importing a Port's vocabulary has violations:\n%s", joinViolations(violations))
	}
}

// Config may be an alias of an internal type and carry methods, since methods
// are not package-scope names, and an …impl may export the error types its
// configuration and startup report.
func TestTiers_AnImplExportingOnlyNewConfigDefaultConfigAndErrorsPasses(t *testing.T) {
	violations := fixtureViolations(t, "extensions/p/pimpl/plugin.go", `package pimpl

import "fixture.test/cog/kernel"

type plugin struct{ config Config }

func New() kernel.Plugin { return &plugin{config: DefaultConfig()} }

func (*plugin) Name() kernel.PluginName                              { return "p" }
func (*plugin) Dependencies() []kernel.PluginName                    { return nil }
func (*plugin) Register(registrar *kernel.Registrar, config any) error { return nil }

type Config struct{ Path string }

func (c Config) WithPath(path string) Config { c.Path = path; return c }

func DefaultConfig() Config { return Config{Path: "values"} }

type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string { return "invalid config" }
`)
	if len(violations) != 0 {
		t.Fatalf("an …impl exporting only what the rule allows has violations:\n%s", joinViolations(violations))
	}
}

// A contract root may act on a *kernel.Registrar it is handed, the way ecs's
// RegisterComponent, ToHandler and ToExecute do: functions that take one, and
// types that are handed one without being a Plugin, are contract. Only a type
// with all three kernel.Plugin methods breaks the rule.
func TestTiers_AContractRootActingOnAHandedRegistrarPasses(t *testing.T) {
	violations := fixtureViolations(t, "bundles/a/register.go", `package a

import "fixture.test/cog/kernel"

type Store struct{}

func RegisterComponent(registrar *kernel.Registrar) *Store { return &Store{} }

func ToHandler(registrar *kernel.Registrar, system any) func() { return func() {} }

type Planner struct{}

func (Planner) Name() kernel.PluginName                              { return Name }
func (*Planner) Register(registrar *kernel.Registrar, config any) error { return nil }
`)
	if len(violations) != 0 {
		t.Fatalf("a contract root acting on a handed Registrar has violations:\n%s", joinViolations(violations))
	}
}

// legacyPlugin is a plugin in the shape from before declaration roots: a
// contract root under a free file name, an …impl and an internal/ package.
var legacyPlugin = map[string]string{
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

func TestTiers_APluginOnTheMigrationListPassesUnderTheLegacyRules(t *testing.T) {
	violations := fixtureViolationsWith(t, append(slices.Clone(fixtureUnmoved), "bundles/q"), legacyPlugin)
	if len(violations) != 0 {
		t.Fatalf("a listed legacy plugin has violations:\n%s", joinViolations(violations))
	}
}

func TestTiers_ALegacyPluginOffTheMigrationListFails(t *testing.T) {
	violations := fixtureViolationsWith(t, fixtureUnmoved, legacyPlugin)
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

func TestTiers_AMigrationEntryNamingNoPackageFails(t *testing.T) {
	violations := fixtureViolationsWith(t, append(slices.Clone(fixtureUnmoved), "bundles/gone"), nil)
	requireOne(t, violations,
		"bundles/gone: bundles/gone is on the migration list and holds no package",
		ruleUnmoved,
	)
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
	if violations := fixtureViolationsWith(t, fixtureUnmoved, files); len(violations) != 0 {
		t.Fatalf("a Slot root holding only allowed files has violations:\n%s", joinViolations(violations))
	}
}

func TestTiers_ARootImportingItsOwnInternalFails(t *testing.T) {
	violations := fixtureViolationsWith(t, fixtureUnmoved, map[string]string{
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

// A legacy extension that is not a Port is implementation, which a moved
// plugin reaches only from tests, as it would a constructor package.
func TestTiers_InternalImportingALegacyExtensionFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/n/internal/reach.go", `package internal

import _ "fixture.test/cog/extensions/w"
`)
	requireOne(t, violations, "bundles/n/internal/reach.go:3", "bundles/n/internal imports extensions/w", ruleReach)
}

// A legacy root sees a constructor package as an …impl.
func TestTiers_ALegacyRootImportingAConstructorFails(t *testing.T) {
	violations := fixtureViolations(t, "bundles/a/reach.go", `package a

import _ "fixture.test/cog/extensions/e/eplugin"
`)
	requireOne(t, violations, "bundles/a/reach.go:3", "bundles/a imports extensions/e/eplugin", ruleImpl)
}
