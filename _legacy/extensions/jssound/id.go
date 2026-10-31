package jssound

import "github.com/dvoyni/cog/extensions/jssound/internal"

// Name is the jssound plugin name and configuration key. It is also the name
// its Device reports, so a game or a test reading sound.Device learns which
// Adapter it got.
const Name = internal.Name
