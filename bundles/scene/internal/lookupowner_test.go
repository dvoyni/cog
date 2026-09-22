package internal

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/modelplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// composeScene builds scene's engine without running it, beside the plugins
// given, and returns it with every error composition reported.
func composeScene(plugins ...kernel.Plugin) (*kernel.Engine, error) {
	var failure error
	all := append([]kernel.Plugin{
		storageplugin.New(), permanentAdapter{}, appplugin.New(), mainLoopAdapter{},
		gfxplugin.New(), backendAdapter{&testBackend{}},
	}, plugins...)
	engine := kernel.New(map[kernel.PluginName]any{storage.Name: storage.Config{}}).
		Handler(func(err error) error { failure = errors.Join(failure, err); return nil }).
		WithPlugins(append(all, New())...)
	return engine, failure
}

// The Lookup is model's resource: the model plugin registers it and scene,
// which draws from it, registers nothing of the kind. A second registration
// would be a second cache and a second upload of every model.
func TestTheLookupIsRegisteredByModelAlone(t *testing.T) {
	engine, err := composeScene(modelplugin.New())
	if err != nil {
		t.Fatalf("composing scene beside model failed: %v", err)
	}
	var owners []kernel.PluginName
	for _, resource := range engine.Describe().Resources {
		if resource.Type == reflect.TypeFor[*model.Lookup]() {
			owners = append(owners, resource.Owner)
		}
	}
	if len(owners) != 1 || owners[0] != model.Name {
		t.Fatalf("*model.Lookup is registered by %v, want %q alone", owners, model.Name)
	}
}

// scene depends on model, so a composition that leaves model out fails rather
// than running a scene with no Lookup to draw from.
func TestSceneWithoutModelFailsComposition(t *testing.T) {
	if _, err := composeScene(); err == nil {
		t.Fatal("scene composed without model, want a missing dependency")
	}
}
