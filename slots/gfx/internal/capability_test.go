package internal

import (
	"github.com/dvoyni/cog/kernel"
)

// callCapture and callFrame call gfx's two capabilities the way the broker
// does: found by name among the ones gfx's Provider offers, and invoked with a
// pointer to the request.
func callCapture(k kernel.Executioner, request captureScreenRequest) (captureScreenResponse, error) {
	return invokeCapability[captureScreenResponse](k, "capture", &request)
}

func callFrame(k kernel.Executioner, request frameSnapshotRequest) (frameSnapshotResponse, error) {
	return invokeCapability[frameSnapshotResponse](k, "frame", &request)
}

func invokeCapability[TResponse any](k kernel.Executioner, name string, request any) (TResponse, error) {
	var zero TResponse
	for _, capability := range (provider{}).Capabilities() {
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
