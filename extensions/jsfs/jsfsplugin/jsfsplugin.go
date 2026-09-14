//go:build js

// Package jsfsplugin constructs the jsfs plugin. Only composition roots and
// tests import it; it is built only for GOOS=js.
package jsfsplugin

import (
	"github.com/dvoyni/cog/extensions/jsfs/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the jsfs plugin. Its jsfs.Config arrives through kernel.New's
// config map under jsfs.Name, and it provides storage's PermanentFS Adapter.
func New() kernel.Plugin { return internal.New() }
