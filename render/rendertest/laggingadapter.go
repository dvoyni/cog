package rendertest

import "github.com/dvoyni/cog/gfx"

// laggingAdapter is a recording adapter whose devices finish their GPU work a
// beat late.
type laggingAdapter struct {
	gfx.BackendAdapter
	harness *Harness
}

// RequestDevice answers the recording adapter's device, lagging.
func (a laggingAdapter) RequestDevice(descriptor gfx.DeviceDescriptor) (gfx.BackendDevice, error) {
	device, err := a.BackendAdapter.RequestDevice(descriptor)
	if err != nil {
		return nil, err
	}
	return laggingDevice{BackendDevice: device, harness: a.harness}, nil
}
