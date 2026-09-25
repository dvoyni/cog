package internal

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// plugin registers app's commands and its capability, and hands its Loop to
// the MainLoop.
type plugin struct {
	// mainLoop is the bound MainLoop Adapter, valid from Start onwards.
	mainLoop kernel.RequiredAdapter[MainLoop]
	// loop is what the MainLoop calls. It is built at Register, once the
	// configuration is known, and never replaced.
	loop *loop
}

// New creates the app plugin. Its app.Config is supplied at Register through
// kernel.New's config map, so New takes no arguments.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins app requires: none. The MainLoop it requires
// is bound at composition and adds no dependency.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register requires the MainLoop, resolves the configuration (nil -> the zero
// app.Config, and every zero field takes its default), builds the Loop, and
// registers the commands and the capability provider.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	p.mainLoop = registrar.RequireAdapter[MainLoopPort]()
	var cfg Config
	if config != nil {
		c, ok := config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
		cfg = c
	}
	p.loop = newLoop(withDefaults(cfg))
	registrar.HandleCommand[QuitCmd](p.quitCmdImpl)
	registrar.HandleCommand[ClipboardWriteCmd](p.clipboardWriteCmdImpl)
	registrar.HandleCommand[TimeCmd](p.timeCmdImpl)
	registrar.ProvideAdapter[McpProvider](mcp.Provider(provider{}))
	return nil
}

// Start hands the Loop to the MainLoop. Every Start runs before the Host's Run,
// so the MainLoop holds its Loop before it enters the platform loop.
func (p *plugin) Start(kernel.Executioner) error {
	p.mainLoop.Get().Attach(p.loop)
	return nil
}
