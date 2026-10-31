package gfx

import "github.com/dvoyni/cog/slots/gfx/internal"

// Name is the gfx plugin's name.
const Name = internal.Name

// PresentOnUpdate is the subscription type of the plugin's end-of-tick present
// handler on app.UpdateEvent; it runs last so gameplay has finished recording.
type PresentOnUpdate = internal.PresentOnUpdate

// RenderOnRender is the subscription type of the plugin's per-frame render
// handler on app.RenderEvent. app publishes app.RenderEvent synchronously on the
// MainLoop's render thread (after the MainLoop makes the surface current), so
// this handler acquires the latest list, translates it, and executes it into
// the backend's screen framebuffer — no explicit render command needed.
type RenderOnRender = internal.RenderOnRender
