package mcp

import "github.com/dvoyni/cog/kernel"

// Name is the broker plugin's name, and therefore the prefix on the broker's
// own tool, mcpserver_architecture. It keeps the spelling it had as a package
// of its own so the tool names an agent sees do not change.
const Name kernel.PluginName = "mcpserver"
