package mcpserver

import "time"

const (
	// DefaultAddr is the host:port the broker binds. 127.0.0.1 is spelled
	// literally rather than as "localhost", because the IPv4/IPv6 mismatch is
	// the classic failure here and the SDK's rebinding protection checks the
	// Host header rather than the bind address.
	//
	// The port number is arbitrary within a band: below the OS ephemeral range
	// (Linux 32768+, Windows 49152+), where a fixed port can otherwise be stolen
	// by an ephemeral allocation, and clear of the common dev ports. What
	// matters is that it is stable across runs, so a game can commit a
	// .mcp.json naming it.
	DefaultAddr = "127.0.0.1:7654"
	// DefaultPath is the single HTTP path the streamable transport is served on.
	DefaultPath = "/mcp"
	// DefaultTimeout bounds how long an agent waits, not how long the engine
	// works.
	DefaultTimeout = 30 * time.Second
)

// Config configures the broker's transport. The zero value means all defaults.
//
// There is no capture output directory, and there will not be one: a field that
// means something only to one provider is that provider's knowledge arriving
// through config, and no provider needs one either, because the agent names
// every path it wants written. There is no enable flag, no auth field and no
// log level: composition is the gate, auth is a different effort, and logging
// is one line at startup.
type Config struct {
	// Addr is the host:port to bind. Empty means DefaultAddr. Binding to
	// localhost is a default rather than a design; an app that changes it has
	// left the scope of the broker's specification.
	Addr string
	// Path is the HTTP path to serve. Empty means DefaultPath.
	Path string
	// Timeout bounds the wait, not the work. It is applied as a deadline on the
	// executioner a capability receives, so it cancels lock acquisition and any
	// cooperative wait — which is what a waiting body does — but Go cannot
	// interrupt a command body that is already running, so a genuinely wedged
	// handler stays wedged. What the deadline buys is that the agent gets a
	// clean mcp.Unavailable in 30 seconds instead of burning five minutes of its
	// session on the client's idle abort. That is a courtesy, not a safety
	// property. Zero means DefaultTimeout.
	Timeout time.Duration
}

// withDefaults fills every unset field, so the rest of the plugin reads a
// Config that is already complete.
func (c Config) withDefaults() Config {
	if c.Addr == "" {
		c.Addr = DefaultAddr
	}
	if c.Path == "" {
		c.Path = DefaultPath
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	return c
}
