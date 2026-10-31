package app

import "github.com/dvoyni/cog/slots/app/internal"

// Config configures the fixed-step loop. It is supplied under Name, and its
// zero value is the default: a zero field takes the default its comment names,
// so a caller sets only what it changes, directly or with the With* builders
// (each returns a modified copy):
//
//	config := map[kernel.PluginName]any{app.Name: app.Config{}.WithStep(time.Second / 30)}
type Config = internal.Config
