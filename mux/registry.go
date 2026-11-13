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
