package recording

import "github.com/dvoyni/cog/gfx"

// adapter is the recording adapter, as Instance.RequestAdapter answers it.
type adapter struct {
	instance *Instance
}

// Info answers AdapterInfo.
func (*adapter) Info() gfx.AdapterInfo { return AdapterInfo }

// Features answers every feature, so any request is honoured.
func (*adapter) Features() gfx.FeatureFlags { return ^gfx.FeatureFlags(0) }

// Limits answers WebGPU's default limits.
func (*adapter) Limits() gfx.Limits { return gfx.DefaultLimits() }

// RequestDevice records the request and answers the recording device.
func (a *adapter) RequestDevice(descriptor gfx.DeviceDescriptor) (gfx.BackendDevice, error) {
	if err := a.instance.record(CallKindRequestDevice, descriptor).Error; err != nil {
		return nil, err
	}
	return &device{instance: a.instance}, nil
}

// Release records the adapter released.
func (a *adapter) Release() { a.instance.record(CallKindReleaseAdapter, nil) }
