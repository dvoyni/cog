// Package rendertest runs render in a real kernel on gfx's recording adapter,
// for render's own tests and those of the plugins that draw with it.
//
// A test makes a Harness, spawns it with render's config and its own plugins,
// which are listed after render, and steps one beat at a time:
//
//	h := rendertest.NewHarness(t)
//	h.Spawn(nil, myplugin.NewPlugin())
//	h.Step()
//
// The harness fails the test on any error the kernel logs, a render.Report
// among them, unless it is a report of a kind the test named with
// ExpectReport.
package rendertest
