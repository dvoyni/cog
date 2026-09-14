package internal

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// plugin registers app's commands and its capability, and hands its Loop to
// the Driver.
type plugin struct {
	// driver is the bound Driver Adapter, valid from Start onwards.
	driver kernel.RequiredAdapter[app.Driver]
	// loop is what the driver calls. It is built at Register, once the
	// configuration is known, and never replaced.
	loop *loop
}

// New creates the app plugin. Its app.Config is supplied at Register through
// kernel.New's config map, so New takes no arguments.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return app.Name }

// Dependencies reports the plugins app requires: none. The Driver it requires
// is bound at composition and adds no dependency.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register requires the Driver, resolves the configuration (nil -> the zero
// app.Config, and every zero field takes its default), builds the Loop, and
// registers the commands and the capability provider.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	p.driver = registrar.RequireAdapter[app.DriverPort]()
	var cfg app.Config
	if config != nil {
		c, ok := config.(app.Config)
		if !ok {
			return app.ErrInvalidConfig{Got: config}
		}
		cfg = c
	}
	p.loop = newLoop(withDefaults(cfg))
	registrar.HandleCommand[app.QuitCmd](p.quitCmdImpl)
	registrar.HandleCommand[app.TimeCmd](p.timeCmdImpl)
	registrar.ProvideAdapter[app.McpProvider](mcp.Provider(provider{}))
	return nil
}

// Start hands the Loop to the driver. Every Start runs before the Host's Run,
// so the driver holds its Loop before it enters its main loop.
func (p *plugin) Start(kernel.Executioner) error {
	p.driver.Get().Attach(p.loop)
	return nil
}
