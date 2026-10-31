package internal

import (
	"fmt"
	"time"

	"github.com/dvoyni/cog/bundles/mcp"
)

const (
	// defaultAddr is the host:port the broker binds. 127.0.0.1 is spelled
	// literally rather than as "localhost", because the IPv4/IPv6 mismatch is
	// the classic failure here and the SDK's rebinding protection checks the
	// Host header rather than the bind address.
	//
	// The port number is arbitrary within a band: below the OS ephemeral range
	// (Linux 32768+, Windows 49152+), where a fixed port can otherwise be stolen
	// by an ephemeral allocation, and clear of the common dev ports. What
	// matters is that it is stable across runs, so a game can commit a
	// .mcp.json naming it.
	defaultAddr = "127.0.0.1:7654"
	// defaultPath is the single HTTP path the streamable transport is served on.
	defaultPath = "/mcp"
	// defaultTimeout bounds how long an agent waits, not how long the engine
	// works.
	defaultTimeout = 30 * time.Second
)

// withDefaults fills every unset field, so the rest of the plugin reads a
// Config that is already complete.
func withDefaults(c mcp.Config) mcp.Config {
	if c.Addr == "" {
		c.Addr = defaultAddr
	}
	if c.Path == "" {
		c.Path = defaultPath
	}
	if c.Timeout == 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

// resolveConfig reads the broker's configuration value: none means every
// default, and anything but a Config is refused.
func resolveConfig(value any) (mcp.Config, error) {
	if value == nil {
		return withDefaults(mcp.Config{}), nil
	}
	config, ok := value.(mcp.Config)
	if !ok {
		return mcp.Config{}, fmt.Errorf("mcpserver: invalid config %T", value)
	}
	return withDefaults(config), nil
}
