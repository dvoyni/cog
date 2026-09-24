package ecs

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// McpProvider is the Adapter through which ecs offers its by-name Commands to
// the mcp broker, as the tools ecs_world, ecs_entity and ecs_query, which read
// the world, and ecs_spawn, ecs_despawn and ecs_update, which write it.
type McpProvider kernel.Adapter[mcp.ProviderPort]
