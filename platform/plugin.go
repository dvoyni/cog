package platform

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
)

// PluginName is platform's plugin name.
const PluginName kernel.PluginName = "platform"

// Plugin is platform: it declares the sockets the window and input
// implementations, the writable filesystem and the read mounts are bound to,
// installs the FS resource over the files, Values, the WindowManager, the
// InputState and Tasks, registers ProcessEvents, and flushes the values when a window
// closes.
type Plugin struct {
	// fileSystem is the FS resource, kept for Start to check its read
	// mounts.
	fileSystem *fileSystem
}

// Config is platform's part of the kernel's config, under PluginName. Where
// windows come from is the implementing plugins' Config, and where files
// come from the adapters the application binds.
type Config struct {
	// ValuesFile is the file in FS the values are kept in; empty is
	// DefaultValuesFile.
	ValuesFile string
}

// NewPlugin returns platform.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies answers none: platform reads no other plugin's resource;
// the implementations it needs come through its sockets.
func (*Plugin) Dependencies() []kernel.PluginName { return nil }

// Init declares the optional exclusive sockets of the window and input
// implementations and of the writable filesystem and the optional socket of
// the read mounts, installs the FS resource, *Values, *WindowManager,
// *InputState and Tasks, and subscribes to WindowClosedEvent to flush the
// values, its Sub kept in the Handlers resource. ProcessEvents is registered
// pinned to the main thread and scheduled nowhere. A config of another type
// than *Config is a composition error, and panics; the kernel reports a
// socket bound wrongly.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	valuesFile := kernel.PluginConfigOrDefault[Config](config).ValuesFile
	if valuesFile == "" {
		valuesFile = DefaultValuesFile
	}
	p.fileSystem = &fileSystem{
		writable: configurator.DeclareExclusiveSocket[WriteFSSocket](true),
		mounts:   configurator.DeclareSocket[ReadMountSocket](true),
	}
	configurator.InitResource[FS](p.fileSystem)
	configurator.InitResource(&Values{file: valuesFile})
	flushOnClose := configurator.RegisterTask2(flushValues)
	configurator.InitResource(Handlers{
		FlushValues: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event WindowClosedEvent) (WindowClosedEvent, kernel.JobHandle) {
			return event, ex.Schedule(flushOnClose, kernel.NoArg{}, in).Handle()
		}),
	})
	configurator.InitResource(&WindowManager{})
	configurator.InitResource(&InputState{})
	configurator.InitResource(&eventProcessor{
		windows:     configurator.DeclareExclusiveSocket[WindowsSocket](true),
		inputSource: configurator.DeclareExclusiveSocket[InputSourceSocket](true),
	})
	tasks := Tasks{ProcessEvents: configurator.RegisterTask3(processEvents)}
	configurator.PinToMainThread(tasks.ProcessEvents)
	configurator.InitResource(tasks)
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
