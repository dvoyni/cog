package types

import "github.com/dvoyni/cog/kernel"

// Name is the ecs plugin's name, as the ecs root declares it. This package
// cannot import the root, which aliases its types, so its tests spell the name
// here; every plugin they compose depends on it.
const Name kernel.PluginName = "ecs"

// authority stands in for the ecs plugin in this package's own tests. They
// cannot compose ecsplugin.New(), because the plugin imports this package, and
// they need this package's unexported state to make the claims they make. It
// registers exactly what the ecs plugin registers - the authority, under Name -
// so every composition here is the one an app builds; the plugin's own tests in
// ecs's internal/ compose the real plugin.
type authority struct {
	// ids is how many Entities the authority reserves room for.
	ids uint32
}

func (authority) Name() kernel.PluginName { return Name }

func (authority) Dependencies() []kernel.PluginName { return nil }

func (a authority) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(newEntities(a.ids))
	return nil
}
