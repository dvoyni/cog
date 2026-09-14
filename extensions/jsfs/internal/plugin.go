//go:build js

package internal

import (
	"errors"
	"strings"
	"syscall/js"

	"github.com/dvoyni/cog/extensions/jsfs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// keyPrefix begins the localStorage key the permanent filesystem is kept under.
const keyPrefix = "cog.storage."

// New creates the jsfs plugin. Its jsfs.Config arrives through kernel.New's
// config map under jsfs.Name. It opens localStorage during Register and
// provides it as storage's PermanentFS Adapter.
func New() kernel.Plugin { return &plugin{} }

type plugin struct{}

func (p *plugin) Name() kernel.PluginName { return jsfs.Name }

func (p *plugin) Dependencies() []kernel.PluginName { return nil }

func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	var cfg jsfs.Config
	if config != nil {
		var ok bool
		cfg, ok = config.(jsfs.Config)
		if !ok {
			return jsfs.ErrInvalidConfig{Got: config}
		}
	}
	appId := cfg.AppId
	if appId == "" || appId == "." || appId == ".." || strings.ContainsAny(appId, `/\`) {
		return jsfs.ErrInvalidAppId{AppId: appId}
	}
	localStorage := js.Global().Get("localStorage")
	if localStorage.IsUndefined() || localStorage.IsNull() {
		return errors.New("jsfs: browser localStorage is unavailable")
	}
	registrar.ProvideAdapter[jsfs.StoragePermanentFS](storage.PermanentFS(&webFS{key: keyPrefix + appId, storage: localStorage}))
	return nil
}
