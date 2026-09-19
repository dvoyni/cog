package archtest

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// mainLoopAdapter provides app's MainLoop to the engines these tests compose, the
// way a platform plugin provides its own: app is a Slot, and a composition
// without one fails. The tests publish app's events by hand, so the Loop app
// attaches is kept by nobody and never driven.
type mainLoopAdapter struct{}

func (mainLoopAdapter) Name() kernel.PluginName           { return "appmainlooptest" }
func (mainLoopAdapter) Dependencies() []kernel.PluginName { return nil }

func (mainLoopAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testAppMainLoop](app.MainLoop(idleMainLoop{}))
	return nil
}

// testAppMainLoop is the Adapter this fixture fills app's MainLoop Port as.
type testAppMainLoop kernel.Adapter[app.MainLoopPort]

// idleMainLoop is a MainLoop with no loop of its own to attach to or stop.
type idleMainLoop struct{}

func (idleMainLoop) Attach(app.Loop)             {}
func (idleMainLoop) ClipboardWrite(string) error { return nil }
func (idleMainLoop) Quit()                       {}
