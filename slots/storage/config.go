package storage

import "github.com/dvoyni/cog/slots/storage/internal"

// DefaultValuesPath is the key-value file used when Config names none.
const DefaultValuesPath = internal.DefaultValuesPath

// Config configures storage's values file. It is supplied under Name, and its
// zero value is the default: DefaultValuesPath. Neither what is read nor what
// persists is configured here: read mounts are contributed through
// ReadMountPort, and the permanent filesystem is the PermanentFS Adapter an
// Extension provides.
type Config = internal.Config
