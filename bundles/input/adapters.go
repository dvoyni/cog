package input

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// McpProvider is the Adapter through which input offers its capabilities to the
// mcp broker.
type McpProvider kernel.Adapter[mcp.ProviderPort]
