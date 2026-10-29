//go:build windows || linux || darwin

// Package internal is the steamplatform plugin: New, and the Backend over the
// Steamworks flat API it provides for platform's BackendPort. Composition roots
// and tests reach New through steamplatformplugin; everything else reaches
// steamplatform through its root.
package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/platform"
)

// plugin is steamplatform: one Adapter, opened at Register.
type plugin struct{}

// New creates the steamplatform plugin. Its steamplatform.Config arrives
// through kernel.New's config map under steamplatform.Name.
func New() kernel.Plugin { return plugin{} }

// Name reports the plugin name.
func (plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins steamplatform requires; it has none. The
// Port it fills binds at composition and adds no dependency.
func (plugin) Dependencies() []kernel.PluginName { return nil }

// Register opens Steam and provides the Backend over it. Steam is opened here
// rather than at Start so that it is up before any plugin creates the window:
// the overlay hooks the graphics device as it is created.
//
// It fails composition on a missing app id, and with ErrRelaunching when Steam
// is starting the game again through itself. A Steam that cannot be reached
// fails nothing: the game runs without it, and the first frame says why.
func (plugin) Register(registrar *kernel.Registrar, config any) error {
	cfg, ok := config.(Config)
	if !ok {
		return ErrInvalidConfig{Got: config}
	}
	if cfg.AppID == 0 {
		return ErrNoAppID{}
	}
	b, err := openBackend(cfg)
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[PlatformBackend](platform.Backend(b))
	return nil
}
