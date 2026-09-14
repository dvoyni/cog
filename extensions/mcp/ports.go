package mcp

import "github.com/dvoyni/cog/kernel"

// ProviderPort is the Port the broker collects every Provider through, zero
// included.
type ProviderPort kernel.CollectedPort[Provider]
