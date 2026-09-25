package internal

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// McpProvider is the Adapter through which app offers its capability, the
// tick source, to the mcp broker.
type McpProvider kernel.Adapter[mcp.ProviderPort]
