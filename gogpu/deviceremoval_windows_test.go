package gogpu_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gogpu"
	"github.com/gogpu/wgpu/hal/dx12/d3d12"
)

func TestARemovedHungOrResetDX12DeviceIsGfxsDeviceLoss(t *testing.T) {
	for _, hresult := range []d3d12.HRESULTError{d3d12.DXGI_ERROR_DEVICE_REMOVED, d3d12.DXGI_ERROR_DEVICE_HUNG, d3d12.DXGI_ERROR_DEVICE_RESET} {
		loss := fmt.Errorf("dx12: device removed after ExecuteCommandLists: %w", hresult)
		if err := gogpu.MarkDeviceLoss(loss); !errors.Is(err, gfx.ErrDeviceLost) || !errors.Is(err, loss) {
			t.Errorf("%v is answered as %v, not as gfx's ErrDeviceLost wrapping it", loss, err)
		}
	}
	other := fmt.Errorf("dx12: Present failed: %w", d3d12.DXGI_ERROR_INVALID_CALL)
	if err := gogpu.MarkDeviceLoss(other); err != other {
		t.Errorf("%v is answered as %v, not as it is", other, err)
	}
}
