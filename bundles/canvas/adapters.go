package canvas

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// McpProvider is the Adapter through which canvas offers its capabilities to the
// mcp broker.
type McpProvider kernel.Adapter[mcp.ProviderPort]
