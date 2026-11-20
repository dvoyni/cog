// Package plugin is cmd/declarebindings' own plugin, listed after render: on
// DeclareEvent it acquires each shader its Config names through render, with
// no defines, unless Config.VariantsOnly is set, and with each define set,
// and hands the binding declaration of each, what gfx reflects of its
// variants, to Config.TakeDeclaration.
package plugin

import (
	"os"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// PluginName is the plugin's name.
const PluginName kernel.PluginName = "declarebindings"

// Config is the plugin's part of the kernel's config, under PluginName: the
// shaders it declares and where it hands their declarations.
type Config struct {
	// Directory is the folder mounted in platform's FS, which ShaderPaths and
	// the shaders' includes are read from; empty means the folder the
	// process runs in.
	Directory string
	// ShaderPaths are the shaders declared, relative to Directory.
	ShaderPaths []string
	// DefineSets are the define sets each shader has a variant declared for,
	// beside its variant with no defines.
	DefineSets [][]render.ShaderDefine
	// VariantsOnly declares each shader's DefineSets alone, leaving out its
	// variant with no defines, for a shader that compiles only with defines,
	// as a 2D material's reading its argument does: a test preprocessing it
	// with a define set none of them is fails on its declaration.
	VariantsOnly bool
	// TakeDeclaration is handed each shader's declaration, the JSON of its
	// binding declaration, by its path, from DeclareEvent's job. It must not
	// be nil.
	TakeDeclaration func(shaderPath string, declaration []byte)
}

// defaultDirectory is Config.Directory when it is empty: the folder the
// process runs in.
const defaultDirectory = "."

// Plugin is the tool's plugin: it mounts Config.Directory in platform's FS,
// installs Handlers and the declaration request, and declares the shaders'
// bindings on DeclareEvent.
type Plugin struct{}

// NewPlugin returns the plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names render, whose Inventory the plugin acquires through,
// and gfx and platform, whose Device and FS it reads.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName, gfx.PluginName, platform.PluginName}
}

// Init mounts Config.Directory, or the folder the process runs in when it
// is empty, in platform's FS; installs the declaration request, Config's
// shaders, define sets, VariantsOnly and TakeDeclaration; and subscribes to DeclareEvent
// the job declaring the shaders' bindings, its Sub kept in the Handlers
// resource. A config of another type than *Config, or a nil TakeDeclaration,
// is a composition error, and panics, as is VariantsOnly with no
// DefineSets, which would declare nothing.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	pluginConfig := kernel.PluginConfigOrDefault[Config](config)
	if pluginConfig.TakeDeclaration == nil {
		panic("declarebindings: Config.TakeDeclaration is nil")
	}
	if pluginConfig.VariantsOnly && len(pluginConfig.DefineSets) == 0 {
		panic("declarebindings: Config.VariantsOnly with no DefineSets declares nothing")
	}
	directory := pluginConfig.Directory
	if directory == "" {
		directory = defaultDirectory
	}
	configurator.BindSocket(platform.ReadMountSocket{FS: os.DirFS(directory)})
	configurator.InitResource(declarationRequest{
		shaderPaths:     pluginConfig.ShaderPaths,
		defineSets:      pluginConfig.DefineSets,
		variantsOnly:    pluginConfig.VariantsOnly,
		takeDeclaration: pluginConfig.TakeDeclaration,
	})
	declareShadersTask := configurator.RegisterTask4(declareShaders)
	configurator.InitResource(Handlers{
		DeclareBindings: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event DeclareEvent) (DeclareEvent, kernel.JobHandle) {
			return event, ex.Schedule(declareShadersTask, kernel.NoArg{}, in).Handle()
		}),
	})
	return nil
}

// Start does nothing.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
