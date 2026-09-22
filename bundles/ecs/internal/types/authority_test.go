package types

import "github.com/dvoyni/cog/kernel"

// Name is the ecs plugin's name, as the ecs root declares it. This package
// cannot import the root, which aliases its types, so its tests spell the name
// here; every plugin they compose depends on it.
const Name kernel.PluginName = "ecs"

// authority stands in for the ecs plugin in this package's own tests. They
// cannot compose ecsplugin.New(), because the plugin imports this package, and
// they need this package's unexported state to make the claims they make. It
// registers what every composition here rests on - the authority, under Name,
// and the shrink Command - so a test of a mechanism is a test of it in an
// engine; the plugin's own tests in ecs's internal/ compose the real plugin.
//
// It deliberately subscribes no drainer. ecs.DrainOnUpdate is a Last() node
// holding write{*Entities} that every real app carries, and putting one in
// every composition here would add a barrier to every benchmark in this package
// whose subject is something else. A test that wants one subscribes it itself,
// which is what an app drain System is anyway; that the ecs plugin subscribes
// one unconditionally is tested where the real plugin is composed.
type authority struct {
	// ids is how many Entities the authority reserves room for.
	ids uint32
}

// shrinkCmd is ecs.ShrinkCmd as this package's tests spell it, for the same
// reason Name is.
type shrinkCmd kernel.Command[ShrinkRequest, ShrinkResponse]

func (authority) Name() kernel.PluginName { return Name }

func (authority) Dependencies() []kernel.PluginName { return nil }

func (a authority) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(newEntities(a.ids))
	registrar.HandleCommand[shrinkCmd](shrinkCommand)
	return nil
}
