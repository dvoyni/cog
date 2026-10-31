package otosound

import "github.com/dvoyni/cog/extensions/otosound/internal"

// Config configures the desktop Adapter. It is supplied under Name, and its
// zero value is the default.
//
// Channel count is not a knob. The output is stereo because panning is: a mono
// output makes constant-power panning meaningless, and a surround output is a
// different Mixer rather than a different config.
//
// Both fields belong to the oto context, and the context is per process. The
// first composition in a process owns it, so a second Engine's values here are
// ignored and reported once as ErrDeviceConfigIgnored; sound.Device then
// reports the values actually in force rather than the ones this Config asked
// for.
type Config = internal.Config
