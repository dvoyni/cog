//go:build js

package internal

import (
	"errors"
	"strings"
	"syscall/js"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// keyPrefix begins the localStorage key the permanent filesystem is kept under.
const keyPrefix = "cog.storage."

// New creates the jsstorage plugin. Its jsstorage.Config arrives through
// kernel.New's config map under jsstorage.Name. It opens localStorage during
// Register and provides it as storage's PermanentFS Adapter.
func New() kernel.Plugin { return &plugin{} }

type plugin struct{}

func (p *plugin) Name() kernel.PluginName { return Name }

func (p *plugin) Dependencies() []kernel.PluginName { return nil }

func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	var cfg Config
	if config != nil {
		var ok bool
		cfg, ok = config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
	}
	appId := cfg.AppId
	if appId == "" || appId == "." || appId == ".." || strings.ContainsAny(appId, `/\`) {
		return ErrInvalidAppId{AppId: appId}
	}
	localStorage := js.Global().Get("localStorage")
	if localStorage.IsUndefined() || localStorage.IsNull() {
		return errors.New("jsstorage: browser localStorage is unavailable")
	}
	registrar.ProvideAdapter[StoragePermanentFS](storage.PermanentFS(&webFS{key: keyPrefix + appId, storage: localStorage}))
	return nil
}
