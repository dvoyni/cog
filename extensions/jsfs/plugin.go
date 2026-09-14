//go:build js

package jsfs

import (
	"errors"
	"fmt"
	"strings"
	"syscall/js"

	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/kernel"
)

// name is the plugin's name. Nothing orders against or configures the plugin by
// name: its Config goes to New.
const name kernel.PluginName = "jsfs"

// keyPrefix begins the localStorage key the permanent filesystem is kept under.
const keyPrefix = "cog.storage."

// Config configures the browser permanent filesystem.
type Config struct {
	// AppId is part of the localStorage key, cog.storage.<AppId>. It must be a
	// single name: not empty, not . or .., and without / or \. A browser has no
	// executable to take a name from, so it is never defaulted.
	AppId string
}

// New creates the jsfs plugin. It opens localStorage during Register and
// provides it as storage's PermanentFS Adapter.
func New(config Config) kernel.Plugin { return &plugin{config: config} }

type plugin struct{ config Config }

func (p *plugin) Name() kernel.PluginName { return name }

func (p *plugin) Dependencies() []kernel.PluginName { return nil }

func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	appId := p.config.AppId
	if appId == "" || appId == "." || appId == ".." || strings.ContainsAny(appId, `/\`) {
		return ErrInvalidAppId{AppId: appId}
	}
	localStorage := js.Global().Get("localStorage")
	if localStorage.IsUndefined() || localStorage.IsNull() {
		return errors.New("jsfs: browser localStorage is unavailable")
	}
	registrar.ProvideAdapter[StoragePermanentFS](storage.PermanentFS(&webFS{key: keyPrefix + appId, storage: localStorage}))
	return nil
}

// ErrInvalidAppId reports an application id that is empty or not a single name.
type ErrInvalidAppId struct{ AppId string }

func (e ErrInvalidAppId) Error() string {
	return fmt.Sprintf("jsfs: invalid app id %q", e.AppId)
}
