package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/bundles/mcp/mcpplugin"
	"github.com/dvoyni/cog/kernel"
)

// The broker renders every tool's schemas in Start and fails there on a type
// its schema library cannot infer or a root that is not an object, so a clean
// start under the real broker proves all three tools render. The archtest
// golden pins what they render to; this is the fast check inside ecs.
func TestTheRealBrokerRendersTheToolsAtStart(t *testing.T) {
	engine := kernel.New(map[kernel.PluginName]any{mcp.Name: mcp.Config{Addr: "127.0.0.1:0"}}).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(New(), newReadFixture(), mcpplugin.New())
	stopped := make(chan struct{})
	t.Cleanup(func() {
		engine.Quit()
		<-stopped
	})
	go func() {
		defer close(stopped)
		engine.Run()
	}()
	<-engine.Ready()
}
