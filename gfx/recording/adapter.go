package recording

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// adapter is the recording adapter, as Instance.RequestAdapter answers it.
type adapter struct {
	instance *Instance
}

// Info answers AdapterInfo.
func (*adapter) Info() gfx.AdapterInfo { return AdapterInfo }

// Features answers every feature but timestamp queries, which wait on the
// wgpu fork.
func (*adapter) Features() gfx.FeatureFlags { return ^gfx.FeatureFlagTimestampQuery }

// Limits answers WebGPU's default limits.
func (*adapter) Limits() gfx.Limits { return gfx.DefaultLimits() }

// RequestDevice records the request and answers the recording device; an
// error when it requires a feature Features lacks, as wgpu does.
func (a *adapter) RequestDevice(descriptor gfx.DeviceDescriptor) (gfx.BackendDevice, error) {
	if err := a.instance.record(CallKindRequestDevice, descriptor, func() error {
		if missing := descriptor.RequiredFeatures &^ a.Features(); missing != 0 {
			return fmt.Errorf("recording: the adapter lacks features %#x", missing)
		}
		return nil
	}).Error; err != nil {
		return nil, err
	}
	return &device{instance: a.instance}, nil
}

// Release records the adapter released.
func (a *adapter) Release() { a.instance.record(CallKindReleaseAdapter, nil, nil) }
