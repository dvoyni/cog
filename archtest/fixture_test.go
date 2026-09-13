package archtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureModule is a miniature cog: one of every kind, wired the way the rules
// allow, so that the clean tree has no violations and each failure test adds
// exactly one.
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
`,
	"libs/l/l.go": `package l

import _ "fixture.test/cog/kernel"
`,
	"slots/s/s.go": `package s

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

type Plugin struct{}

func (Plugin) Name() kernel.PluginName                            { return a.Name }
func (Plugin) Dependencies() []kernel.PluginName                  { return nil }
func (Plugin) Register(registrar *kernel.Registrar, config any) error { return nil }
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

import _ "fixture.test/cog/extensions/p"
`,
	"extensions/w/w.go": `package w

import (
	_ "fixture.test/cog/bundles/b"
	_ "fixture.test/cog/extensions/p"
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

// fixtureViolations checks a fixture tree after confirming that the clean tree
// passes, so a failure test can only be failing on the file it added.
func fixtureViolations(t *testing.T, name, body string) []violation {
	t.Helper()
	requireGo(t)
	root := writeFixture(t, fixtureModule)
	if clean := check(t, root); len(clean) != 0 {
		t.Fatalf("the clean fixture has violations:\n%s", joinViolations(clean))
	}
	addFile(t, root, name, body)
	return check(t, root)
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
