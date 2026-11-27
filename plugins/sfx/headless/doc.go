// Package headless holds an sfx.BackendSocket that plays nothing, for tests
// and for runs no one hears: NewBackend answers a backend whose device is
// ready, named "headless", at zero latency, which accepts every flush's
// changes and keeps none of them. Its Prepare reads a clip's Ogg headers for
// its rate, channels, length and loop region, decoding no samples, so a voice
// under it still ends when its clip says it would. It reports nothing. It is
// an adapter, not a plugin: a test's plugin, or an application's own, makes
// it and binds it in its Init.
//
//	func (p *headlessPlugin) Init(c *kernel.Configurator, config any) error {
//		c.BindSocket[sfx.BackendSocket](headless.NewBackend(0))
//		return nil
//	}
//
// See ../docs/design.md.
package headless
