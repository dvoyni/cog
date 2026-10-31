package platform

import "github.com/dvoyni/cog/slots/platform/internal"

// DefaultStoreInterval is the shortest gap between two uploads when Config
// names none.
const DefaultStoreInterval = internal.DefaultStoreInterval

// Config configures platform. It is supplied under Name, and its zero value is
// the default: uploads at most every DefaultStoreInterval.
type Config = internal.Config
