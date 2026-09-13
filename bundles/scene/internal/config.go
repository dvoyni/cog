package internal

// Config is scene's configuration. It is declared here because the Lookup
// resource carries it, and sceneimpl re-exports it as sceneimpl.Config.
type Config struct {
	// PoseSampleRate is the global animation bake rate in Hz: every clip of
	// every model is baked to pose rows at this rate at load. It must be
	// positive.
	PoseSampleRate int
}

// DefaultConfig is the configuration scene runs with when it is given none, and
// what fills a field a caller left zero.
func DefaultConfig() Config { return Config{PoseSampleRate: 60} }
