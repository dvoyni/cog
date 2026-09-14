package wgpu

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// GfxBackend is the Adapter through which wgpu fills gfx's backend Port.
type GfxBackend kernel.Adapter[gfx.BackendPort]

// McpProvider is the Adapter through which wgpu offers its capabilities to the
// mcp broker.
type McpProvider kernel.Adapter[mcp.ProviderPort]
