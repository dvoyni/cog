//go:build !js

// Package otosoundplugin constructs the otosound plugin. Only composition roots
// and tests import it; it is built only for desktop platforms (!js), so a web
// build composing otosound fails to compile rather than quietly shipping a
// Mixer that starves.
package otosoundplugin

import (
	"github.com/dvoyni/cog/extensions/otosound/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the otosound plugin. Its otosound.Config arrives through
// kernel.New's config map under otosound.Name, and it fills sound.BackendPort.
func New() kernel.Plugin { return internal.New() }
