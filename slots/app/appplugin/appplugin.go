// Package appplugin constructs the app plugin. Only composition roots and tests
// import it; everything else reaches app through its root.
package appplugin

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app/internal"
)

// New creates the app plugin. Its app.Config arrives through kernel.New's
// config map under app.Name, and it requires exactly one Adapter for
// app.DriverPort, which a driver such as wgpu provides.
func New() kernel.Plugin { return internal.New() }
