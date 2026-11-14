package rendertest

import "github.com/dvoyni/cog/gfx"

// harnessAdapter is a recording adapter whose devices are harnessDevices.
type harnessAdapter struct {
	gfx.BackendAdapter
	harness *Harness
}

// RequestDevice answers the recording adapter's device, wrapped.
func (a harnessAdapter) RequestDevice(descriptor gfx.DeviceDescriptor) (gfx.BackendDevice, error) {
	device, err := a.BackendAdapter.RequestDevice(descriptor)
	if err != nil {
		return nil, err
	}
	return harnessDevice{BackendDevice: device, harness: a.harness}, nil
}
