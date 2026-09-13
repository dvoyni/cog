package mcp

import "github.com/dvoyni/cog/kernel"

// Provider is a plugin that offers capabilities to an agent. It speaks cog
// contracts only; it never emits protocol vocabulary.
//
// There is one interface rather than one per kind of capability, so adding a
// kind edits neither this package nor the broker. Embedding kernel.Plugin gives
// the broker the plugin name it namespaces tool names with, without a second
// lookup.
//
// Capabilities are static for the engine lifetime: the broker asks once, at its
// own Start, and a type assertion answers whether a plugin provides at all. A
// provider with nothing to offer right now says so inside its capability — an
// empty list, or an Unavailable from a call — never by ceasing to satisfy this
// interface. Empty, not absent.
type Provider interface {
	kernel.Plugin

	// Capabilities reports what this plugin offers an agent. It is called
	// exactly once, during the broker's Start.
	Capabilities() []Capability
}
