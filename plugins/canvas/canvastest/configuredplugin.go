package canvastest

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
)

// configuredPlugin is canvas, initialized with the config the harness was
// spawned with in place of the kernel's, which rendertest's harness fills
// for its own plugins alone.
type configuredPlugin struct {
	*canvas.Plugin
	config *canvas.Config
}

// Init initializes canvas with the harness's config.
func (p configuredPlugin) Init(configurator *kernel.Configurator, _ any) error {
	return p.Plugin.Init(configurator, p.config)
}
