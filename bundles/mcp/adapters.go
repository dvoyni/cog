package mcp

import "github.com/dvoyni/cog/kernel"

// McpProvider is the Adapter through which mcp offers its capabilities to the
// mcp broker.
type McpProvider kernel.Adapter[ProviderPort]
