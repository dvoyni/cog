package ecs

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// McpProvider is the Adapter through which ecs offers its three read Commands
// to the mcp broker, as the tools ecs_world, ecs_entity and ecs_query.
type McpProvider kernel.Adapter[mcp.ProviderPort]
