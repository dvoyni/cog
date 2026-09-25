package gogpu

import "github.com/dvoyni/cog/extensions/gogpu/internal"

// ErrInvalidConfig is returned by the plugin's Register when the config value
// handed to it is neither nil nor a gogpu.Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrDepthOnlyPassUnsupported reports a pass this backend declined to encode.
type ErrDepthOnlyPassUnsupported = internal.ErrDepthOnlyPassUnsupported

// ErrBindGroupRefused reports a bind group the device would not create, which
// leaves the draw encoded with nothing bound for that group. It is the backstop
// beneath gfx's own checks: a binding gfx never emitted is caught before it
// gets here, but a binding it emitted against a buffer this backend no longer
// holds - released, or from a device ago - is visible only at the point the
// group is built.
//
// It is reported once per shader and group. A refused group is a property of
// the material rather than of the HAL, so a second site is a second report.
type ErrBindGroupRefused = internal.ErrBindGroupRefused
