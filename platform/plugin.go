package platform

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
)

// PluginName is platform's plugin name.
const PluginName kernel.PluginName = "platform"

// Plugin is platform: it finds the Windows and InputSource the implementing
// plugins installed, declares the sockets the writable filesystem and the
// read mounts are bound to, installs the FS resource over them, Values, the
// WindowManager and Tasks, registers ProcessEvents, and flushes the values
// when a window closes.
type Plugin struct {
	// fileSystem is the FS resource, kept for Start to check its read
	// mounts.
	fileSystem *fileSystem
}

// Config is platform's part of the kernel's config, under PluginName. Where
// windows and files come from is the implementing plugins' Config.
type Config struct {
	// ValuesFile is the file in FS the values are kept in; empty is
	// DefaultValuesFile.
	ValuesFile string
}

// NewPlugin returns platform.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Init finds Windows and InputSource, installed by plugins listed before
// platform, declares the exclusive socket of the writable filesystem and the
// socket of the read mounts, installs the FS resource, *Values,
// *WindowManager and Tasks, and subscribes to WindowClosedEvent to flush the
// values. ProcessEvents is registered pinned to the main thread and
// scheduled nowhere. A config of another type than Config, or a seam no plugin
// installed, is a composition error, and panics; so is a seam two plugins
// installed or bound, which the kernel refuses.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	var configuration Config
	if config != nil {
		var ok bool
		if configuration, ok = config.(Config); !ok {
			panic(fmt.Sprintf("platform: config is a %T, want a platform.Config", config))
		}
	}
	if configuration.ValuesFile == "" {
		configuration.ValuesFile = DefaultValuesFile
	}
	windows, ok := configurator.Resource[Windows]()
	if !ok || windows == nil {
		panic("platform: no plugin before platform installs platform.Windows")
	}
	inputSource, ok := configurator.Resource[InputSource]()
	if !ok || inputSource == nil {
		panic("platform: no plugin before platform installs platform.InputSource")
	}
	p.fileSystem = &fileSystem{
		writable: configurator.DeclareExclusiveSocket[WriteFSSocket](false),
		mounts:   configurator.DeclareSocket[ReadMountSocket](true),
	}
	configurator.InitResource[FS](p.fileSystem)
	configurator.InitResource(&Values{file: configuration.ValuesFile})
	flushValues := configurator.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, filesystem kernel.Write[FS], values kernel.Write[*Values]) kernel.NoResult {
		if err := values.Get().Flush(ElevateFileAccess(filesystem)); err != nil {
			k.Logger().LogErr(err)
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event WindowClosedEvent) (WindowClosedEvent, kernel.JobHandle) {
		return event, ex.Schedule(flushValues, kernel.NoArg{}, in).Handle()
	})
	configurator.InitResource(&WindowManager{})
	pump := &pump{windows: windows, inputSource: inputSource}
	processEvents := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Write[*WindowManager]) kernel.NoResult {
		pump.run(k, manager.Get())
		return kernel.NoResult{}
	})
	configurator.PinToMainThread(processEvents)
	configurator.InitResource(Tasks{ProcessEvents: processEvents})
	return nil
}

// Start checks the read mounts bound: one without an FS is a
// composition error, and panics.
func (p *Plugin) Start(*kernel.Kernel) error {
	for _, mount := range p.fileSystem.mounts.GetAll() {
		if mount.FS == nil {
			panic(fmt.Sprintf("platform: a read mount at priority %d has no FS", mount.Priority))
		}
	}
	return nil
}

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
