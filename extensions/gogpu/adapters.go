package gogpu

import "github.com/dvoyni/cog/extensions/gogpu/internal"

// AppMainLoop is the Adapter through which gogpu fills app's MainLoop Port: the
// gogpu library's main loop, which drives the Loop app attaches.
type AppMainLoop = internal.AppMainLoop

// GfxBackend is the Adapter through which gogpu fills gfx's backend Port.
type GfxBackend = internal.GfxBackend
