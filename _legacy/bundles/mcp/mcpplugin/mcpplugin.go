// Package mcpplugin constructs the mcp broker plugin. Only composition roots and
// tests import it; everything else reaches mcp through its root.
package mcpplugin

import (
	"github.com/dvoyni/cog/bundles/mcp/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the broker. Configure its transport with an mcp.Config under
// mcp.Name. It requires no Adapter: it collects every Adapter provided for
// mcp.ProviderPort and contributes its own mcp.McpProvider.
func New() kernel.Plugin { return internal.New() }
