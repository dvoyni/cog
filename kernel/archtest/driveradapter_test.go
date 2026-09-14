package archtest

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// driverAdapter provides app's Driver to the engines these tests compose, the
// way a platform plugin provides its own: app is a Slot, and a composition
// without one fails. The tests publish app's events by hand, so the Loop app
// attaches is kept by nobody and never driven.
type driverAdapter struct{}

func (driverAdapter) Name() kernel.PluginName           { return "appdrivertest" }
func (driverAdapter) Dependencies() []kernel.PluginName { return nil }

func (driverAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testAppDriver](app.Driver(idleDriver{}))
	return nil
}

// testAppDriver is the Adapter this fixture fills app's Driver Port as.
type testAppDriver kernel.Adapter[app.DriverPort]

// idleDriver is a Driver with no loop of its own to attach to or stop.
type idleDriver struct{}

func (idleDriver) Attach(app.Loop) {}
func (idleDriver) Quit()           {}
