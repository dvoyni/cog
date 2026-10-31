package gogpu

import "github.com/dvoyni/cog/extensions/gogpu/internal"

// Config configures the gogpu driver's window. The fixed step it drives is
// app's Config, not this one. It is supplied under Name, and its zero value is
// the default: a zero field takes the default its comment names, so a caller
// sets only what it changes, directly or with the With* builders (each returns
// a modified copy):
//
//	cfg := gogpu.Config{}.WithTitle("Feuds").WithSize(1600, 900)
//
// Fields are exported so a Config can be serialized. The switches that default
// to on are spelled as their negation (NoResize, NoVSync), so that off is the
// zero value too.
type Config = internal.Config
