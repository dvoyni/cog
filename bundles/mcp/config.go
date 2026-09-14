package mcp

import "time"

// Config configures the broker's transport. It arrives through kernel.New's
// config map under Name, and a zero field takes its default, so a caller names
// only what it changes:
//
//	kernel.New(map[kernel.PluginName]any{mcp.Name: mcp.Config{Addr: "127.0.0.1:7655"}})
//
// There is no capture output directory, and there will not be one: a field that
// means something only to one provider is that provider's knowledge arriving
// through config, and no provider needs one either, because the agent names
// every path it wants written. There is no enable flag, no auth field and no
// log level: composition is the gate, auth is a different effort, and logging
// is one line at startup.
type Config struct {
	// Addr is the host:port to bind. Empty means 127.0.0.1:7654. Binding to
	// localhost is a default rather than a design; an app that changes it has
	// left the scope of the broker's specification.
	Addr string
	// Path is the HTTP path to serve. Empty means /mcp.
	Path string
	// Timeout bounds the wait, not the work. It is applied as a deadline on the
	// executioner a capability receives, so it cancels lock acquisition and any
	// cooperative wait — which is what a waiting body does — but Go cannot
	// interrupt a command body that is already running, so a genuinely wedged
	// handler stays wedged. What the deadline buys is that the agent gets a
	// clean Unavailable in 30 seconds instead of burning five minutes of its
	// session on the client's idle abort. That is a courtesy, not a safety
	// property. Zero means 30 seconds.
	Timeout time.Duration
}
