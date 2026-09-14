//go:build !js

// Package diskstorageplugin constructs the diskstorage plugin. Only composition
// roots and tests import it; it is built only for desktop platforms (!js).
package diskstorageplugin

import (
	"github.com/dvoyni/cog/extensions/diskstorage/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the diskstorage plugin. Its diskstorage.Config arrives through
// kernel.New's config map under diskstorage.Name, and it provides storage's
// PermanentFS Adapter.
func New() kernel.Plugin { return internal.New() }
