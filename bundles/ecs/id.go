package ecs

import "github.com/dvoyni/cog/kernel"

// Name is the ecs plugin's name and configuration key. A plugin that registers
// Components or Systems declares a dependency on it, because that is what
// registers the authority first.
const Name kernel.PluginName = "ecs"
