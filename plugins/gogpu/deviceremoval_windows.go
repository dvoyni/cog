package gogpu

import "github.com/gogpu/wgpu/hal/dx12/dxgi"

// isDeviceRemoved answers whether err is DX12's removed device, as wgpu's
// dxgi.IsDeviceRemovedError judges it.
func isDeviceRemoved(err error) bool { return dxgi.IsDeviceRemovedError(err) }
