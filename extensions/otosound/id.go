package otosound

import "github.com/dvoyni/cog/kernel"

// Name is the otosound plugin name and configuration key. It is also the name
// its Device reports, so a game or a test reading sound.Device learns which
// Adapter it got.
const Name kernel.PluginName = "otosound"
