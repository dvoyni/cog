package gfx

// PluginEdges is the part of the gfx plugin that still names mcp: its
// Capabilities. The plugin lives in gfximpl, and mcp is not a Port yet, so
// gfximpl may not import it; the contract root may, and gfximpl's plugin embeds
// this to offer them. It is not for any other use, and it goes away when mcp
// becomes a Port (dvoyni/cog#335) and the capabilities move into gfximpl.
type PluginEdges struct{}
