package internal

// Config configures the Steam Adapter. It is supplied under Name. Its zero
// value is not usable: AppID is required.
type Config struct {
	// AppID is the game's Steam app id. A release build launched outside
	// Steam is relaunched through Steam under it.
	AppID uint32
	// ResetStats clears every stat and achievement the Steam account holds for
	// the game when the plugin registers. It is for development: run it
	// together with wiping the game's local data, or the game's catch-up
	// uploads everything again on the same start.
	ResetStats bool
}

// WithAppID sets the game's Steam app id.
func (c Config) WithAppID(appID uint32) Config {
	c.AppID = appID
	return c
}

// WithResetStats sets whether the account's stats and achievements are
// cleared at start.
func (c Config) WithResetStats(reset bool) Config {
	c.ResetStats = reset
	return c
}
