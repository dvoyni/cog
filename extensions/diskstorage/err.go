package diskstorage

import "github.com/dvoyni/cog/extensions/diskstorage/internal"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrInvalidAppId reports an application id that is not a single directory name.
type ErrInvalidAppId = internal.ErrInvalidAppId
