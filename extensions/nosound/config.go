package nosound

import "github.com/dvoyni/cog/extensions/nosound/internal"

// Config configures the silent Adapter. It is supplied under Name, and its zero
// value is the default.
//
// There is no buffer size and no channel count. Nothing is buffered, and the
// output is stereo because panning is: a mono output would make constant-power
// panning meaningless, and a surround output is a different Mixer.
type Config = internal.Config
