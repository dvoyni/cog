package jsstorage

import "github.com/dvoyni/cog/extensions/jsstorage/internal"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrInvalidAppId reports an application id that is empty or not a single name.
type ErrInvalidAppId = internal.ErrInvalidAppId
