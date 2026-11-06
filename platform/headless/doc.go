// Package headless holds a platform.WindowsSocket without an operating
// system, for tests: NewWindows answers windows that open and close as
// platform asks, with zero native handles, so platform publishes
// WindowOpenedEvent and WindowClosedEvent and a backend that sees a zero
// handle draws without a window. It is an adapter, not a plugin: the test's
// own plugin makes it and binds it in its Init.
//
//	func (p *test) Init(c *kernel.Configurator, _ any) error {
//		c.BindSocket[platform.WindowsSocket](headless.NewWindows())
//		return nil
//	}
//
// See ../docs/design.md.
package headless
