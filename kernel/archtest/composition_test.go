package archtest

import (
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/extensions/gfx"

	"github.com/dvoyni/cog/bundles/anim/animplugin"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene/ecssceneimpl"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/bundles/mcp/mcpplugin"
	"github.com/dvoyni/cog/bundles/scene/sceneimpl"
	"github.com/dvoyni/cog/bundles/ui/uiimpl"
	"github.com/dvoyni/cog/extensions/gfx/gfximpl"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// Every Bundle and Port in cog, composed with the test Adapters gfx and storage
// require, so the description names every Resource, Port type and interface,
// Adapter type, command and subscription cog declares. Nothing runs: composition is what Describe reads.
func TestTypeName_NamesEveryTypeInAFullCogCompositionUniquely(t *testing.T) {
	var failure error
	engine := kernel.New(map[kernel.PluginName]any{storage.Name: storage.Config{}}).
		Handler(func(err error) bool { failure = errors.Join(failure, err); return false }).
		WithPlugins(
			storageplugin.New(), permanentAdapter{}, gfximpl.New(), backendAdapter{&detachedBackend{}},
			inputplugin.New(), animplugin.New(), canvasplugin.New(), sceneimpl.New(), uiimpl.New(),
			ecsplugin.New(), ecssceneimpl.New(), mcpplugin.New(),
		)
	if failure != nil {
		t.Fatalf("composing every plugin failed: %v", failure)
	}

	rendered := map[string][]reflect.Type{}
	for _, typ := range describedTypes(engine.Describe()) {
		name := kernel.TypeName(typ)
		if !slices.Contains(rendered[name], typ) {
			rendered[name] = append(rendered[name], typ)
		}
	}
	if len(rendered) < 50 {
		t.Fatalf("the description names only %d types; is every plugin composed?", len(rendered))
	}
	for name, types := range rendered {
		if len(types) > 1 {
			paths := make([]string, 0, len(types))
			for _, typ := range types {
				stars := ""
				for typ.Kind() == reflect.Pointer && typ.Name() == "" {
					stars, typ = stars+"*", typ.Elem()
				}
				paths = append(paths, stars+typ.PkgPath()+"."+typ.Name())
			}
			t.Errorf("%s names %d distinct types: %s", name, len(types), strings.Join(paths, ", "))
		}
		if strings.Contains(name, "internal.") {
			t.Errorf("%s names a package no caller imports", name)
		}
	}
}

// describedTypes is every type an ArchitectureDescription names, repeats
// included. The conflict report names only types already listed elsewhere.
func describedTypes(description kernel.ArchitectureDescription) []reflect.Type {
	var types []reflect.Type
	for _, resource := range description.Resources {
		types = append(types, resource.Type)
	}
	for _, port := range description.Ports {
		types = append(types, port.Type, port.Interface)
		for _, adapter := range port.Adapters {
			types = append(types, adapter.Type)
		}
	}
	for _, command := range description.Commands {
		types = append(types, command.Type)
		types = append(types, command.Reads...)
		types = append(types, command.Writes...)
		types = append(types, command.Uses...)
	}
	for _, subscription := range description.Subscriptions {
		types = append(types, subscription.Event, subscription.Type)
		types = append(types, subscription.DependsOn...)
		types = append(types, subscription.Reads...)
		types = append(types, subscription.Writes...)
		types = append(types, subscription.Uses...)
	}
	return types
}

// backendAdapter provides a Backend to gfx, the way a driver provides its own:
// gfx is a Port, and a composition without one fails.
type backendAdapter struct{ backend gpu.Backend }

func (backendAdapter) Name() kernel.PluginName           { return "gfxbackendtest" }
func (backendAdapter) Dependencies() []kernel.PluginName { return nil }

func (a backendAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testGfxBackend](a.backend)
	return nil
}

// testGfxBackend is the Adapter this fixture fills gfx's backend Port as.
type testGfxBackend kernel.Adapter[gfx.BackendPort]

// detachedBackend is a Backend whose device never arrives. Nothing here runs,
// so only the ids a plugin may take at registration are implemented.
type detachedBackend struct {
	gpu.Backend
	next atomic.Uint32
}

func (*detachedBackend) Ready() bool                 { return false }
func (b *detachedBackend) NewTexture() gpu.TextureID { return gpu.TextureID(b.next.Add(1)) }
func (b *detachedBackend) NewBuffer() gpu.BufferID   { return gpu.BufferID(b.next.Add(1)) }

// permanentAdapter provides storage's PermanentFS: an empty filesystem that
// reads nothing and refuses writes.
type permanentAdapter struct{}

func (permanentAdapter) Name() kernel.PluginName           { return "test-permanent-fs" }
func (permanentAdapter) Dependencies() []kernel.PluginName { return nil }

func (permanentAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testPermanentFS](storage.PermanentFS(emptyPermanentFS{}))
	return nil
}

// testPermanentFS is the Adapter this fixture fills storage's permanent
// filesystem Port as.
type testPermanentFS kernel.Adapter[storage.PermanentFSPort]

type emptyPermanentFS struct{ fstest.MapFS }

func (emptyPermanentFS) WriteFile(string, []byte, fs.FileMode) error { return errors.ErrUnsupported }
func (emptyPermanentFS) MkdirAll(string, fs.FileMode) error          { return errors.ErrUnsupported }
func (emptyPermanentFS) Remove(string) error                         { return errors.ErrUnsupported }
func (emptyPermanentFS) Rename(string, string) error                 { return errors.ErrUnsupported }
