package gfx

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the gfx plugin's name.
const Name kernel.PluginName = "gfx"

// PresentOnUpdate is the subscription type of the plugin's end-of-tick present
// handler on app.UpdateEvent; it runs last so gameplay has finished recording.
type PresentOnUpdate kernel.Subscription[app.UpdateEvent]

// RenderOnRender is the subscription type of the plugin's per-frame render
// handler on app.RenderEvent. app publishes app.RenderEvent synchronously on the
// MainLoop's render thread (after the MainLoop makes the surface current), so
// this handler acquires the latest list, translates it, and executes it into
// the backend's screen framebuffer — no explicit render command needed.
type RenderOnRender kernel.Subscription[app.RenderEvent]
