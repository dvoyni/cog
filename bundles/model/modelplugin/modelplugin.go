// Package modelplugin constructs the model plugin. Only composition roots and
// tests import it; everything else reaches model through its root.
package modelplugin

import (
	"github.com/dvoyni/cog/bundles/model/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the model plugin. Configure it with a model.Config under
// model.Name; it requires no Adapter, and storage registered before it.
func New() kernel.Plugin { return internal.New() }
