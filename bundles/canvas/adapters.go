package canvas

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// McpProvider is the Adapter through which canvas offers its capabilities to the
// mcp broker.
type McpProvider kernel.Adapter[mcp.ProviderPort]

// StorageReadMount is the Adapter through which canvas contributes its built-in
// shaders and default font to storage as a read mount.
type StorageReadMount kernel.Adapter[storage.ReadMountPort]
