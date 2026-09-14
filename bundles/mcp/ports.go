package mcp

import "github.com/dvoyni/cog/kernel"

// Provider is the Adapter the broker collects: a value that offers
// capabilities to an agent. It speaks cog contracts only; it never emits
// protocol vocabulary.
//
// A plugin declares its McpProvider Adapter type for ProviderPort and
// contributes one from Register:
//
//	registrar.ProvideAdapter[McpProvider](mcp.Provider(provider{}))
//
// There is one interface rather than one per kind of capability, so adding a
// kind edits neither this package nor the broker. It carries no name: the
// broker namespaces tool names by the PluginName of the plugin that contributed
// it, which the engine records when it binds the Adapter.
//
// Capabilities are static for the engine lifetime: the broker asks once, at its
// own Start. A provider with nothing to offer right now says so inside its
// capability — an empty list, or an Unavailable from a call — never by
// withdrawing the Adapter. Empty, not absent.
type Provider interface {
	// Capabilities reports what this plugin offers an agent. It is called
	// exactly once, during the broker's Start.
	Capabilities() []Capability
}

// ProviderPort is the Port the broker collects every Provider through, zero
// included.
type ProviderPort kernel.CollectedPort[Provider]
