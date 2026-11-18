package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
)

// testPlugin is what every test plugin embeds: listed after canvas, and
// doing nothing on Start, Stop and Destroy.
type testPlugin struct{}

func (testPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.PluginName}
}

func (testPlugin) Start(*kernel.Kernel) error { return nil }

func (testPlugin) Stop(*kernel.Kernel) error { return nil }

func (testPlugin) Destroy() error { return nil }
