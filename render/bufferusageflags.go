package render

import "github.com/dvoyni/cog/gfx"

// BufferUsageFlags is the set of ways a loaded or made buffer may be used,
// each flag gfx's of the same name, declared when the buffer is acquired or
// made. Every buffer also carries gfx's copy destination, which render's own
// writes need; maps are not offered, since render maps no buffer. A buffer
// used in a way its flags lack is refused by gfx.
type BufferUsageFlags uint64

const (
	BufferUsageFlagCopySrc  = BufferUsageFlags(gfx.BufferUsageFlagCopySrc)
	BufferUsageFlagIndex    = BufferUsageFlags(gfx.BufferUsageFlagIndex)
	BufferUsageFlagVertex   = BufferUsageFlags(gfx.BufferUsageFlagVertex)
	BufferUsageFlagUniform  = BufferUsageFlags(gfx.BufferUsageFlagUniform)
	BufferUsageFlagStorage  = BufferUsageFlags(gfx.BufferUsageFlagStorage)
	BufferUsageFlagIndirect = BufferUsageFlags(gfx.BufferUsageFlagIndirect)
)
