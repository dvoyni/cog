package wgpu

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
)

// AppMainLoop is the Adapter through which wgpu fills app's MainLoop Port: the
// gogpu main loop, which drives the Loop app attaches.
type AppMainLoop kernel.Adapter[app.MainLoopPort]

// GfxBackend is the Adapter through which wgpu fills gfx's backend Port.
type GfxBackend kernel.Adapter[gfx.BackendPort]
