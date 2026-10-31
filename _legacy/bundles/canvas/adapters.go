package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal"

// McpProvider is the Adapter through which canvas offers its capabilities to the
// mcp broker.
type McpProvider = internal.McpProvider

// StorageReadMount is the Adapter through which canvas contributes its built-in
// shaders and default font to storage as a read mount.
type StorageReadMount = internal.StorageReadMount
