package gfximpl

import (
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/mcp"
	"github.com/dvoyni/cog/kernel"
)

// The plugin offers gfx's capabilities through the PluginEdges it embeds, so
// the broker finds it among the providers.
var _ mcp.Provider = (*Plugin)(nil)

// captureScreen and frameSnapshot call gfx's two capabilities the way the
// broker does: found by name among the plugin's, and invoked with a pointer to
// the request.
func captureScreen(k kernel.Executioner, request gfx.CaptureRequest) (gfx.CaptureResponse, error) {
	return invokeCapability[gfx.CaptureResponse](k, "capture", &request)
}

func frameSnapshot(k kernel.Executioner, request gfx.FrameRequest) (gfx.FrameResponse, error) {
	return invokeCapability[gfx.FrameResponse](k, "frame", &request)
}

func invokeCapability[TResponse any](k kernel.Executioner, name string, request any) (TResponse, error) {
	var zero TResponse
	for _, capability := range New().Capabilities() {
		if capability.Name() != name {
			continue
		}
		response, err := capability.Invoke(k, request)
		if err != nil {
			return zero, err
		}
		return response.(TResponse), nil
	}
	panic("gfx offers no capability named " + name)
}
