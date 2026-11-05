package platform

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
)

// PluginName is platform's plugin name.
const PluginName kernel.PluginName = "platform"

// Plugin is platform: it finds the Windows and InputSource the implementing
// plugins installed, installs the WindowManager and the Pump, and registers ProcessEvents.
type Plugin struct{}

// Config is platform's part of the kernel's config, under PluginName. It has
// nothing to configure yet; where windows and files come from is the
// implementing plugins' Config.
type Config struct{}

// NewPlugin returns platform.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Init finds Windows and InputSource, installed by plugins listed before
// platform, and installs *WindowManager and Pump. ProcessEvents
// is registered pinned to the main thread and scheduled nowhere. A config of
// another type than Config, or a seam no plugin installed, is a composition
// error, and panics; so is a seam two plugins installed, which the kernel
// refuses.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	if config != nil {
		if _, ok := config.(Config); !ok {
			panic(fmt.Sprintf("platform: config is a %T, want a platform.Config", config))
		}
	}
	windows, ok := configurator.Resource[Windows]()
	if !ok || windows == nil {
		panic("platform: no plugin before platform installs platform.Windows")
	}
	inputSource, ok := configurator.Resource[InputSource]()
	if !ok || inputSource == nil {
		panic("platform: no plugin before platform installs platform.InputSource")
	}
	configurator.InitResource(&WindowManager{})
	pump := &pump{windows: windows, inputSource: inputSource}
	processEvents := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Write[*WindowManager]) kernel.NoResult {
		pump.run(k, manager.Get())
		return kernel.NoResult{}
	})
	configurator.PinToMainThread(processEvents)
	configurator.InitResource(Pump{ProcessEvents: processEvents})
	return nil
}

// Start does nothing.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
