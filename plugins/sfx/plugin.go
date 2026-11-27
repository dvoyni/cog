package sfx

import (
	"cmp"
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
)

// PluginName is sfx's plugin name.
const PluginName kernel.PluginName = "sfx"

// defaultMaxVoices is Config.MaxVoices when it is zero.
const defaultMaxVoices = 64

// defaultMaxBuses is Config.MaxBuses when it is zero.
const defaultMaxBuses = 32

// Plugin is sfx: it declares the BackendSocket a backend binds, and installs
// Tasks, the Queue, LiveVoices, the ClipTable, BusVolumes, the Listener and
// DeviceView. It subscribes to nothing.
type Plugin struct {
	// backend is the socket of the bound backend, and maxVoices the cap Init
	// resolved, for Start to tell the backend how many slots exist.
	backend   kernel.ExclusiveSocket[BackendSocket]
	maxVoices int
}

// Config is sfx's part of the kernel's config, under PluginName.
type Config struct {
	// MaxVoices is how many voices may exist at once; zero means 64. A play
	// that finds every slot taken ends with ReasonStolen.
	MaxVoices int
	// MaxBuses is how many buses exist, Master among them; zero means 32. A
	// bus outside them is Master.
	MaxBuses int
}

// NewPlugin returns sfx.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names platform, whose FS the flush reads a clip's bytes
// through; sfx is listed after it. A plugin recording into sfx, or
// scheduling its flush, is listed after sfx, since its Init reads sfx.Tasks.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{platform.PluginName}
}

// Init declares the required BackendSocket, so a kernel no backend binds
// panics in Spawn, and installs the Queue, LiveVoices, the ClipTable,
// BusVolumes, the Listener, DeviceView, the flush's private resource and
// Tasks, Flush registered and scheduled nowhere. A config of another type
// than *Config, or a negative MaxVoices or MaxBuses, is a composition error,
// and panics.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	pluginConfig := kernel.PluginConfigOrDefault[Config](config)
	if pluginConfig.MaxVoices < 0 {
		panic(fmt.Sprintf("sfx: Config.MaxVoices is %d, and it may not be negative", pluginConfig.MaxVoices))
	}
	if pluginConfig.MaxBuses < 0 {
		panic(fmt.Sprintf("sfx: Config.MaxBuses is %d, and it may not be negative", pluginConfig.MaxBuses))
	}
	p.maxVoices = cmp.Or(pluginConfig.MaxVoices, defaultMaxVoices)
	maxBuses := cmp.Or(pluginConfig.MaxBuses, defaultMaxBuses)
	p.backend = configurator.DeclareExclusiveSocket[BackendSocket](false)
	configurator.InitResource(makeQueue(p.maxVoices, maxBuses))
	configurator.InitResource(makeLiveVoices(p.maxVoices))
	configurator.InitResource(makeClipTable())
	configurator.InitResource(makeBusVolumes(maxBuses))
	configurator.InitResource(makeListener())
	configurator.InitResource(deviceView{})
	configurator.InitResource(&flushState{backend: p.backend})
	configurator.InitResource(Tasks{Flush: configurator.RegisterTask8(flush)})
	return nil
}

// Start tells the bound backend how many slots exist, before any flush.
func (p *Plugin) Start(*kernel.Kernel) error {
	p.backend.Get().Voices(p.maxVoices)
	return nil
}

// Stop does nothing: sfx publishes nothing for the voices still live when
// the kernel is destroyed.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
