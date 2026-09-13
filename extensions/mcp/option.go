package mcp

// Option configures a capability at construction. The list is variadic on both
// constructors so later options do not break every provider, and the value it
// writes to is unexported so the set of options stays closed to this package.
type Option func(*capabilitySettings)

// capabilitySettings collects what the options set before the Capability is
// built.
type capabilitySettings struct {
	readOnly bool
}

// ReadOnly states that this capability does not change the game. That is a fact
// about a cog command rather than protocol vocabulary, so a provider may state
// it and the broker translates it into the protocol's read-only hint.
//
// The question every client actually uses the hint for is "safe to
// auto-approve?", and cog answers it in those terms: a capability that writes
// only the file the agent named is still read-only.
func ReadOnly() Option {
	return func(settings *capabilitySettings) { settings.readOnly = true }
}
