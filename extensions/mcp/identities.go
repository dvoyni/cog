package mcp

import "github.com/dvoyni/cog/kernel"

// Name is the broker plugin's name, and therefore the prefix on the broker's
// own tool, mcpserver_architecture. The broker lives in mcpimpl; its name is
// declared here, with the rest of the contract, and it keeps the spelling it
// had as a package of its own so the tool names an agent sees do not change.
const Name kernel.PluginName = "mcpserver"
