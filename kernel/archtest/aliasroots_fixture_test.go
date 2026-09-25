package archtest

import "testing"

// The fixtures for ADR 0003's alias-index root: bundles/a declares everything
// in its internal/, which never imports the root, and its root only aliases.

// aliasPlugin is a clean alias-index Bundle: a Component with a method in
// internal/, a vocabulary type in internal/types, an Adapter its internal/
// provides, an error, Name and an ordering identity, and a forwarder.
var aliasPlugin = map[string]string{
	"bundles/a/doc.go": `// Package a is an alias-index Bundle.
package a
`,
	"bundles/a/id.go": `package a

import "fixture.test/cog/bundles/a/internal"

const Name = internal.Name

type StepOnUpdate = internal.StepOnUpdate
`,
	"bundles/a/components.go": `package a

import "fixture.test/cog/bundles/a/internal"

// Body is a Component.
type Body = internal.Body
`,
	"bundles/a/types.go": `package a

import "fixture.test/cog/bundles/a/internal/types"

type Layer = types.Layer

const LayersAll = types.LayersAll
`,
	"bundles/a/adapters.go": `package a

import "fixture.test/cog/bundles/a/internal"

type ReadMount = internal.ReadMount
`,
	"bundles/a/err.go": `package a

import "fixture.test/cog/bundles/a/internal"

type ErrNoBody = internal.ErrNoBody

var ErrClosed = internal.ErrClosed
`,
	"bundles/a/utils.go": `package a

import "fixture.test/cog/bundles/a/internal"

func NewBody(mass float64) Body { return internal.NewBody(mass) }
`,
	"bundles/a/internal/types/layer.go": `package types

type Layer uint32

const LayersAll Layer = ^Layer(0)
`,
	"bundles/a/internal/plugin.go": `package internal

import (
	"errors"

	"fixture.test/cog/bundles/a/internal/types"
	"fixture.test/cog/kernel"
)

const Name kernel.PluginName = "a"

type StepOnUpdate struct{}

type ReadMount kernel.Adapter[int]

type ErrNoBody struct{}

func (ErrNoBody) Error() string { return "no body" }

var ErrClosed = errors.New("closed")

// Body is a Component whose fields are all exported.
type Body struct {
	InvMass float64
	Layers  types.Layer
}

// Mass reads the inverse back.
func (b Body) Mass() float64 { return 1 / b.InvMass }

func NewBody(mass float64) Body { return Body{InvMass: 1 / mass} }

type plugin struct{}

func New() kernel.Plugin { return plugin{} }

func (plugin) Name() kernel.PluginName           { return Name }
func (plugin) Dependencies() []kernel.PluginName { return nil }
func (plugin) Register(registrar *kernel.Registrar, config any) error {
	registrar.ProvideAdapter[ReadMount](0)
	return nil
}
`,
	"bundles/a/aplugin/aplugin.go": `package aplugin

import (
	"fixture.test/cog/bundles/a/internal"
	"fixture.test/cog/kernel"
)

func New() kernel.Plugin { return internal.New() }
`,
}

// aliasViolationsWith checks the fixture with bundles/a added and held to the
// alias-index rules, and then with files added or replaced.
func aliasViolationsWith(t *testing.T, files map[string]string) []violation {
	t.Helper()
	aliasIndexRoots["bundles/a"] = true
	t.Cleanup(func() { delete(aliasIndexRoots, "bundles/a") })
	requireGo(t)
	root := writeFixture(t, fixtureModule)
	for name, body := range aliasPlugin {
		addFile(t, root, name, body)
	}
	if clean := check(t, root); len(clean) != 0 {
		t.Fatalf("the clean alias-index fixture has violations:\n%s", joinViolations(clean))
	}
	for name, body := range files {
		addFile(t, root, name, body)
	}
	return check(t, root)
}

func TestTiers_TheCleanAliasIndexFixturePasses(t *testing.T) {
	aliasViolationsWith(t, nil)
}

func TestTiers_AnAliasIndexRootDeclaringATypeFails(t *testing.T) {
	violations := aliasViolationsWith(t, map[string]string{"bundles/a/types.go": `package a

import "fixture.test/cog/bundles/a/internal/types"

type Layer = types.Layer

const LayersAll = types.LayersAll

type Point struct{ X, Y int }
`})
	requireOne(t, violations, "bundles/a/types.go:9", "bundles/a declares Point", ruleAliasDeclarations)
}

func TestTiers_AnAliasIndexAliasRenamingItsTargetFails(t *testing.T) {
	violations := aliasViolationsWith(t, map[string]string{"bundles/a/types.go": `package a

import (
	"fixture.test/cog/bundles/a/internal"
	"fixture.test/cog/bundles/a/internal/types"
)

type Layer = types.Layer

const LayersAll = types.LayersAll

type Rigid = internal.Body
`})
	requireOne(t, violations, "bundles/a/types.go:12", "bundles/a declares Rigid", ruleAliasDeclarations)
}

func TestTiers_AnAliasIndexConstantOfItsOwnFails(t *testing.T) {
	violations := aliasViolationsWith(t, map[string]string{"bundles/a/id.go": `package a

import "fixture.test/cog/bundles/a/internal"

const Name = "a"

type StepOnUpdate = internal.StepOnUpdate
`})
	requireOne(t, violations, "bundles/a/id.go:5", "bundles/a declares Name", ruleAliasDeclarations)
}

// A root importing its internal and an internal importing the root would not
// compile, so a package under internal/ that the root does not reach carries
// the edge: the one shape of it the compiler lets through.
func TestTiers_AnAliasIndexInternalImportingItsRootFails(t *testing.T) {
	violations := aliasViolationsWith(t, map[string]string{"bundles/a/internal/extra/reach.go": `package extra

import _ "fixture.test/cog/bundles/a"
`})
	requireOne(t, violations, "bundles/a/internal/extra/reach.go:3", "bundles/a/internal/extra imports bundles/a", ruleAliasInternal)
}

func TestTiers_AnAliasIndexForwarderIntoItsInternalPasses(t *testing.T) {
	if violations := aliasViolationsWith(t, nil); len(violations) != 0 {
		t.Fatalf("violations:\n%s", joinViolations(violations))
	}
}

// A separable part of internal/ may be its own package under it, and the root
// aliases and forwards into it exactly as it does into internal/.
func TestTiers_AnAliasIndexRootReachingAnInternalSubPackagePasses(t *testing.T) {
	violations := aliasViolationsWith(t, map[string]string{
		"bundles/a/internal/shape/shape.go": `package shape

type Circle struct{ Radius float64 }

func NewCircle(radius float64) Circle { return Circle{Radius: radius} }
`,
		"bundles/a/types.go": `package a

import (
	"fixture.test/cog/bundles/a/internal/shape"
	"fixture.test/cog/bundles/a/internal/types"
)

type Layer = types.Layer

const LayersAll = types.LayersAll

type Circle = shape.Circle
`,
		"bundles/a/utils.go": `package a

import (
	"fixture.test/cog/bundles/a/internal"
	"fixture.test/cog/bundles/a/internal/shape"
)

func NewBody(mass float64) Body { return internal.NewBody(mass) }

func NewCircle(radius float64) Circle { return shape.NewCircle(radius) }
`,
	})
	if len(violations) != 0 {
		t.Fatalf("violations:\n%s", joinViolations(violations))
	}
}

// internal/types is for plain data: a layer mask and its constant pass, an
// error's Error method passes, and a function fails.
func TestTiers_AnAliasIndexTypesPackageHoldingLogicFails(t *testing.T) {
	violations := aliasViolationsWith(t, map[string]string{"bundles/a/internal/types/logic.go": `package types

type ErrFull struct{}

func (ErrFull) Error() string { return "full" }

func Bit(i uint) Layer { return 1 << i }
`})
	requireOne(t, violations, "bundles/a/internal/types/logic.go:7", "bundles/a/internal/types declares Bit, which is logic", ruleTypesData)
}
