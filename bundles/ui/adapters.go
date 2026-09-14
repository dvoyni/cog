package ui

import (
	"github.com/dvoyni/cog/extensions/mcp"
	"github.com/dvoyni/cog/kernel"
)

// McpProvider is the Adapter through which ui offers its capabilities to the
// mcp broker.
type McpProvider kernel.Adapter[mcp.ProviderPort]
