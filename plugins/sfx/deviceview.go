package sfx

// deviceView is what DeviceView names: the Device the backend answered when
// the last flush polled it.
type deviceView struct{ device Device }

// Device answers the device as of the last flush: whether anything is
// audible, which backend got it, and its rate, channels and latency. Before
// the first flush it is the zero Device, not ready, since nothing has polled
// the backend yet.
func (v deviceView) Device() Device { return v.device }
