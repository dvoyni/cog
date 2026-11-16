package rendertest

import "github.com/dvoyni/cog/gfx"

// harnessAdapter is the wrapped instance's adapter, whose devices are
// harnessDevices.
type harnessAdapter struct {
	gfx.BackendAdapter
	harness *Harness
}

// RequestDevice answers the wrapped adapter's device, wrapped.
func (a harnessAdapter) RequestDevice(descriptor gfx.DeviceDescriptor) (gfx.BackendDevice, error) {
	device, err := a.BackendAdapter.RequestDevice(descriptor)
	if err != nil {
		return nil, err
	}
	return harnessDevice{BackendDevice: device, harness: a.harness}, nil
}
