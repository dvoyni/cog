package jsstorage

import "github.com/dvoyni/cog/extensions/jsstorage/internal"

// Config configures the browser permanent filesystem. It is supplied under
// Name. A browser has no executable to take a name from, so its zero value, an
// empty AppId, fails Register with ErrInvalidAppId.
type Config = internal.Config
