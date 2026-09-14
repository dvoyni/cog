//go:build !js

// Package diskfsplugin constructs the diskfs plugin. Only composition roots and
// tests import it; it is built only for desktop platforms (!js).
package diskfsplugin

import (
	"github.com/dvoyni/cog/extensions/diskfs/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the diskfs plugin. Its diskfs.Config arrives through kernel.New's
// config map under diskfs.Name, and it provides storage's PermanentFS Adapter.
func New() kernel.Plugin { return internal.New() }
