//go:build js

// Package jssoundplugin constructs the jssound plugin. Only composition roots
// and tests import it; it is built only for GOOS=js, so a desktop build
// composing jssound fails to compile rather than opening a Web Audio context
// that is not there.
package jssoundplugin

import (
	"github.com/dvoyni/cog/extensions/jssound/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the jssound plugin. Its jssound.Config arrives through
// kernel.New's config map under jssound.Name, and it fills sound.BackendPort.
func New() kernel.Plugin { return internal.New() }
