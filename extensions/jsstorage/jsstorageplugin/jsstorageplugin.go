//go:build js

// Package jsstorageplugin constructs the jsstorage plugin. Only composition
// roots and tests import it; it is built only for GOOS=js.
package jsstorageplugin

import (
	"github.com/dvoyni/cog/extensions/jsstorage/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the jsstorage plugin. Its jsstorage.Config arrives through
// kernel.New's config map under jsstorage.Name, and it provides storage's
// PermanentFS Adapter.
func New() kernel.Plugin { return internal.New() }
