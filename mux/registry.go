package mux

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// registry is mux's kernel resource: what is registered in one kernel, so two
// kernels share nothing.
type registry struct {
	// models holds each registered model by its type.
	models map[reflect.Type]*modelRegistration
	// easings holds the kernel's easings, mux's and the game's, which every
	// timeline of the kernel eases by.
	easings *easingTable
	// interpolators holds each interpolator a game registered, as an
	// interpolator[T], by T.
	interpolators map[reflect.Type]any
	// sealed is set once every plugin has initialized.
	sealed bool
}

// findRegistry answers the registry of configurator's kernel, for the
// registration that format and args describe. It panics if mux's plugin has
// not initialized yet, or the kernel has gone past Init.
func findRegistry(configurator *kernel.Configurator, format string, args ...any) *registry {
	found, ok := configurator.Resource[*registry]()
	if !ok {
		panic("mux: " + fmt.Sprintf(format, args...) + " before the mux plugin")
	}
	if found.sealed {
		panic("mux: " + fmt.Sprintf(format, args...) + " after Init")
	}
	return found
}

// seal ends registration, once every plugin has initialized: each path of a
// property whose type a game registered an interpolator for takes that one,
// in place of the one built in, and a registration from here on panics.
func (r *registry) seal() {
	for _, registration := range r.models {
		for _, path := range registration.paths {
			if registeredInterpolator, ok := r.interpolators[path.propertyType]; ok {
				path.interpolator = registeredInterpolator
			}
		}
	}
	r.sealed = true
}
