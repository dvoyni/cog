package mux

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// RegisterModel registers model M, whose addresses hold up to as many holes as
// the depth D, an [N]int64 the game declares: type GameDepth [12]int64. It
// installs three resources: the model as *M, holding M's zero value, its
// Timeline[M, D], and Tasks[M, D]. It is called from the Init of a plugin
// listed after mux's.
//
// Registering before mux's plugin, after Init, twice, or with a D that is not
// an array of int64, is a composition error, and panics.
func RegisterModel[M any, D comparable](configurator *kernel.Configurator) {
	modelType := reflect.TypeFor[M]()
	registry := findRegistry(configurator, "model %v is registered", modelType)
	if _, ok := registry.models[modelType]; ok {
		panic(fmt.Sprintf("mux: model %v is registered twice", modelType))
	}
	depthType := reflect.TypeFor[D]()
	if depthType.Kind() != reflect.Array || depthType.Elem() != reflect.TypeFor[int64]() {
		panic(fmt.Sprintf("mux: model %v is registered with depth %v, which is not an array of int64", modelType, depthType))
	}
	registry.models[modelType] = &modelRegistration{depthType: depthType}
	configurator.InitResource(new(M))
	configurator.InitResource(Timeline[M, D]{timelineSide[M, D]{state: &timelineState[M, D]{easings: registry.easings}}})
	configurator.InitResource(Tasks[M, D]{Step: configurator.RegisterTask2(step[M, D])})
}

// modelRegistration is what the registry keeps of one registered model.
type modelRegistration struct {
	// depthType is the model's depth, an [N]int64: an address of the model
	// holds up to N holes.
	depthType reflect.Type
	// paths holds every path made for the model, in the order made.
	paths []*compiledPath
}
