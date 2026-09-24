//go:build ecs_validate

package internal

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// hooks.md's padding rule, checked in a validating build: a Component some
// reader watches for Changed has no implicit padding. A release build does not
// check, and there implicit padding can record a Changed no field made, never
// miss one; TestExplicitPaddingRecordsNoChangedForEqualFieldValues counts that
// in a release build and asserts nothing about it.

// nestedImplicit holds implicitWide's padding one struct down.
type nestedImplicit struct {
	Before int64
	Inner  implicitWide
}

// arrayImplicit holds implicitWide's padding in each array element.
type arrayImplicit struct {
	Items [2]implicitWide
}

// tailImplicit has no gap between its fields and 6 bytes after the last.
type tailImplicit struct {
	Value int64
	Small int16
}

// trailingEmpty ends in a zero-size field, which Go pads after so the field's
// address stays inside the struct: 7 bytes after Flag and Empty.
type trailingEmpty struct {
	Value int64
	Flag  bool
	Empty struct{}
}

// nestedTail holds tailImplicit's tail padding one struct down, with a field
// after it.
type nestedTail struct {
	Inner tailImplicit
	After int64
}

// layoutsPlugin owns the Components with implicit padding.
type layoutsPlugin struct{}

func (layoutsPlugin) Name() kernel.PluginName { return "layouts" }

func (layoutsPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{typesName} }

func (layoutsPlugin) Register(registrar *kernel.Registrar, _ any) error {
	RegisterComponent[implicitWide](registrar, 8)
	RegisterComponent[nestedImplicit](registrar, 8)
	RegisterComponent[arrayImplicit](registrar, 8)
	RegisterComponent[tailImplicit](registrar, 8)
	RegisterComponent[trailingEmpty](registrar, 8)
	RegisterComponent[nestedTail](registrar, 8)
	RegisterComponent[paddedWide](registrar, 8)
	return nil
}

type layoutReaderSystem kernel.Subscription[app.UpdateEvent]

// composeLayoutReader composes a world with one System, and returns the
// composition failure, or nil.
func composeLayoutReader(system any) error {
	var failure error
	kernel.New(nil).
		Handler(func(err error) error { failure = err; return err }).
		WithPlugins(
			authority{ids: 8},
			&componentsPlugin{ids: 8},
			layoutsPlugin{},
			&systemsPlugin{
				deps: []kernel.PluginName{typesName, "components", "layouts"},
				subscribe: func(registrar *kernel.Registrar) {
					registrar.Subscribe[layoutReaderSystem](ToHandler[app.UpdateEvent](registrar, system))
				},
			},
		)
	return failure
}

// TestAChangedReaderOfAComponentWithImplicitPaddingFailsComposition is the
// check: a reader watching Changed, under HookAddedChanged or HookAll, of a
// Component whose layout has implicit padding anywhere, flat, nested, in an
// array or at the tail, fails composition naming the Component, where the
// padding is, how many bytes, and the _ [N]byte field that fixes it.
func TestAChangedReaderOfAComponentWithImplicitPaddingFailsComposition(t *testing.T) {
	for _, arm := range []struct {
		name   string
		system any
		want   []string
	}{
		{"flat", func(h *Hooks[implicitWide, HookAddedChanged]) {},
			[]string{"implicitWide has 7 bytes of implicit padding after field A:", "_ [7]byte"}},
		{"nested", func(h *Hooks[nestedImplicit, HookAll]) {},
			[]string{"nestedImplicit has 7 bytes of implicit padding after field Inner.A:", "_ [7]byte"}},
		{"array", func(h *Hooks[arrayImplicit, HookAddedChanged]) {},
			[]string{"arrayImplicit has 7 bytes of implicit padding after field Items[_].A:", "_ [7]byte"}},
		{"tail", func(h *Hooks[tailImplicit, HookAll]) {},
			[]string{"tailImplicit has 6 bytes of implicit padding at the end:", "_ [6]byte"}},
		{"a trailing zero-size field", func(h *Hooks[trailingEmpty, HookAddedChanged]) {},
			[]string{"trailingEmpty has 7 bytes of implicit padding at the end:", "_ [7]byte"}},
		{"tail, nested", func(h *Hooks[nestedTail, HookAddedChanged]) {},
			[]string{"nestedTail has 6 bytes of implicit padding at the end of Inner:", "_ [6]byte"}},
	} {
		failure := composeLayoutReader(arm.system)
		if failure == nil {
			t.Fatalf("%s: a Changed reader of a Component with implicit padding composed", arm.name)
		}
		for _, want := range append(arm.want, "systems", "watches", "for Changed") {
			if !strings.Contains(failure.Error(), want) {
				t.Fatalf("%s: composition failure %q does not name %q", arm.name, failure.Error(), want)
			}
		}
	}
}

// TestAReaderNotWatchingChangedDoesNotCheckPadding is the other half of the
// rule: padding is a Changed compare's concern alone, so every kind set without
// Changed composes over a Component with implicit padding, as does a Changed
// reader of a Component whose padding is spelled out.
func TestAReaderNotWatchingChangedDoesNotCheckPadding(t *testing.T) {
	for _, arm := range []struct {
		name   string
		system any
	}{
		{"HookSpawned", func(h *Hooks[implicitWide, HookSpawned]) {}},
		{"HookDespawned", func(h *Hooks[implicitWide, HookDespawned]) {}},
		{"HookSpawnedDespawned", func(h *Hooks[nestedImplicit, HookSpawnedDespawned]) {}},
		{"HookAdded", func(h *Hooks[arrayImplicit, HookAdded]) {}},
		{"HookRemoved", func(h *Hooks[tailImplicit, HookRemoved]) {}},
		{"HookAddedRemoved", func(h *Hooks[implicitWide, HookAddedRemoved]) {}},
		{"a writer alone", func(q *Query[fieldOf[trailingEmpty]], s *Set[implicitWide]) {}},
		{"explicit padding under HookAll", func(h *Hooks[paddedWide, HookAll]) {}},
	} {
		if failure := composeLayoutReader(arm.system); failure != nil {
			t.Fatalf("%s: composition failed: %v", arm.name, failure)
		}
	}
}
