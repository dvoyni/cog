package steamplatform

import "github.com/dvoyni/cog/extensions/steamplatform/internal"

// Config configures the Steam Adapter. It is supplied under Name. Its zero
// value is not usable: AppID is required. ResetStats clears the account's stats
// and achievements at start, for development, together with wiping the game's
// local data.
type Config = internal.Config
