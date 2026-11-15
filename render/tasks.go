package render

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// beginFrame keeps frame as the one being recorded, for Frame's calls.
func beginFrame(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState]) kernel.NoResult {
	state.Get().setRecordingFrame(frame)
	return kernel.NoResult{}
}

// writeLoads reads the file of each buffer loaded and not yet written, and
// decodes the body of each such texture, one after another, and writes it,
// so gfx's next submit carries it, and requests the mips of the textures
// loaded with Mipmaps.
func writeLoads(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	table.Get().loadedBuffers.writePending(table.Get(), device.Get(), files.Get())
	state.Get().mips.request(table.Get().loadedTextures.writePending(table.Get(), device.Get(), files.Get())...)
	return kernel.NoResult{}
}

// generateMips generates, in render's own command list of frame, the mips of
// every texture requested since it last ran, so gfx's submit carries them
// before every user's list.
func generateMips(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	state.Get().mips.generate(table.Get(), device.Get(), files.Get(), frame)
	return kernel.NoResult{}
}

// endFrame ends the frame gfx has presented: it frees the loaded textures
// and buffers whose last acquire was released and the made ones released,
// releases the bind groups its mips drew with, takes back the temporary
// textures held, frees those gone unused for Config's TemporaryIdleFrames
// frames, and then unbinds every material value naming an asset it freed.
func endFrame(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device]) kernel.NoResult {
	frameState := state.Get()
	table.Get().freeReleased(device.Get())
	frameState.mips.endFrame(device.Get())
	frameState.temporaryTextures.endFrame(frameState.temporaryIdleFrames, table.Get(), device.Get())
	table.Get().unbindFreed(device.Get())
	return kernel.NoResult{}
}
