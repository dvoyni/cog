package diskstorage

import "github.com/dvoyni/cog/extensions/diskstorage/internal"

// Config configures the desktop permanent filesystem. It is supplied under
// Name, and its zero value is the default: the executable's name as the
// application id.
type Config = internal.Config
