package rendertest_test

import (
	"bytes"
	"slices"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// loseAndRecover loses the device at the next submit, steps the beat that
// loses it and the beat that recovers it, and waits for a loaderPlugin's
// onRecovered to send on recoveredCallCounts how many calls were recorded when it ran.
// It answers the index of the recovery's first call, its request of a new
// device, and that count.
func loseAndRecover(t *testing.T, h *rendertest.Harness, recoveredCallCounts <-chan int) (int, int) {
	t.Helper()
	h.ExpectLoggedError(gfx.ErrDeviceLost)
	h.AnswerNext(recording.CallKindSubmit, recording.Answer{Error: gfx.ErrDeviceLost})
	h.Step()
	callCountAfterLoss := len(h.Calls())
	h.Step()
	var callCountAtRecovered int
	h.WaitUntil("the RecoveredEvent", func() bool {
		select {
		case callCountAtRecovered = <-recoveredCallCounts:
			return true
		default:
			return false
		}
	})
	requests := findCallIndices(h.Calls()[callCountAfterLoss:], recording.CallKindRequestDevice)
	if len(requests) != 1 {
		t.Fatalf("the beat after the loss requested %d devices, want one", len(requests))
	}
	return callCountAfterLoss + requests[0], callCountAtRecovered
}

// recordCallCount answers an onRecovered sending on recoveredCallCounts how many calls
// h has recorded.
func recordCallCount(h *rendertest.Harness, recoveredCallCounts chan<- int) func() {
	return func() { recoveredCallCounts <- len(h.Calls()) }
}

// containsWrite answers whether one of calls is a write of data, into a buffer
// or a texture.
func containsWrite(calls []recording.Call, data []byte) bool {
	return slices.ContainsFunc(calls, func(call recording.Call) bool {
		written, ok := readWrittenData(call.Arguments)
		return ok && bytes.Equal(written, data)
	})
}

// readWrittenData answers the data a call of arguments wrote, into a buffer
// or a texture; false for a call that is no write.
func readWrittenData(arguments any) ([]byte, bool) {
	switch arguments := arguments.(type) {
	case recording.WriteTextureArguments:
		return arguments.Data, true
	case recording.WriteBufferArguments:
		return arguments.Data, true
	}
	return nil, false
}

// containsAssetWriteBetween answers whether one of writes, calls on one asset,
// came after call start and before call end.
func containsAssetWriteBetween[H comparable](writes []assetCall[H], start, end int) bool {
	return slices.ContainsFunc(writes, func(write assetCall[H]) bool { return write.index > start && write.index < end })
}

func TestAfterADeviceLossRenderWritesItsAssetsAgainUnderTheirHandlesBeforeRecoveredEvent(t *testing.T) {
	h := rendertest.NewHarness(t)
	recoveredCallCounts := make(chan int, 1)
	albedoPNG, albedoTexels := makePNG(t, 4, 4, 255)
	vertexBytes := []byte{1, 2, 3, 4, 5, 6}
	files := makeDrawFiles(t)
	putFile(t, files, "material.wgsl", []byte("// material.wgsl"))
	putFile(t, files, "material.wgsl.json", []byte(materialDeclaration))
	putFile(t, files, "albedo.png", albedoPNG)
	putFile(t, files, "crate.vertices", vertexBytes)
	setTint := tint{Color: [4]float32{1, 0.5, 0.25, 1}, Strength: 2}
	var scene parameterScene
	var albedo render.Texture
	var vertices render.Buffer
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex == 0 {
				scene = makeParameterScene(inventory)
				albedo = inventory.AcquireTexture("albedo.png", render.TextureOptions{Mipmaps: true})
				vertices = inventory.AcquireBuffer("crate.vertices", render.BufferUsageFlagVertex)
				inventory.SetMaterialUniform(scene.material, "tint", setTint)
			}
			recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
				pass.SetMaterial(scene.material)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			})
		},
		onRecovered: recordCallCount(h, recoveredCallCounts),
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Step()

	recoveryCallIndex, callCountAtRecovered := loseAndRecover(t, h, recoveredCallCounts)
	h.Step()

	calls := h.Calls()
	recoveryCalls := calls[recoveryCallIndex:callCountAtRecovered]
	for _, want := range []struct {
		what    string
		written bool
		data    []byte
	}{
		{what: "the loaded texture's texels", written: containsAssetWriteBetween(readTextureCalls(calls).find(recording.CallKindWriteTexture, albedo), recoveryCallIndex, callCountAtRecovered), data: albedoTexels},
		{what: "the loaded buffer's bytes", written: containsAssetWriteBetween(readBufferCalls(calls).find(recording.CallKindWriteBuffer, vertices), recoveryCallIndex, callCountAtRecovered), data: append(slices.Clone(vertexBytes), 0, 0)},
		{what: "the white default texture", written: true, data: bytes.Repeat([]byte{0xff}, 24)},
		{what: "the default depth texture", written: true, data: bytes.Repeat([]byte{0xff}, 12)},
		{what: "the material's uniform block", written: true, data: unsafe.Slice((*byte)(unsafe.Pointer(&setTint)), unsafe.Sizeof(setTint))},
	} {
		if !want.written || !containsWrite(recoveryCalls, want.data) {
			t.Errorf("%s was not written again between the recovery, at call %d, and RecoveredEvent, at %d", want.what, recoveryCallIndex, callCountAtRecovered)
		}
	}

	frames := h.RecordedFrames()
	buffers := readSubmittedBuffers(frames[len(frames)-1])
	if len(buffers) == 0 {
		t.Fatal("the frame after the recovery submitted no command list")
	}
	viewLevels, groupLevels := readTextureLevels(calls)
	want := []mipPass{{target: textureLevel{albedo, 1, 0}, source: textureLevel{albedo, 0, 0}}, {target: textureLevel{albedo, 2, 0}, source: textureLevel{albedo, 1, 0}}}
	if got := readMipPasses(buffers[0], viewLevels, groupLevels); !slices.Equal(got, want) {
		t.Errorf("the frame after the recovery's first list, render's own, drew %v, want the loaded texture's mips, %v", got, want)
	}
}

func TestALoadedAssetWhoseMemoryMountBytesWereRemovedIsReportedOnceAfterADeviceLossAndOneFailedBeforeIsNot(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	recoveredCallCounts := make(chan int, 1)
	generatedPNG, _ := makePNG(t, 2, 2, 255)
	files := makeDrawFiles(t)
	putFile(t, files, "generated.png", generatedPNG)
	putFile(t, files, "generated.bin", []byte{1, 2, 3, 4})
	putFile(t, files, "vanishing.bin", []byte{1, 2, 3, 4})
	putFile(t, files, "vanishing.png", generatedPNG)
	// vanishing.bin and vanishing.png are acquired after the first frame,
	// and removed before the next frame writes them, so their writes fail.
	var generated, missing render.Texture
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			if frameIndex == 0 {
				generated = inventory.AcquireTexture("generated.png", render.TextureOptions{})
				missing = inventory.AcquireTexture("missing.png", render.TextureOptions{})
				inventory.AcquireBuffer("generated.bin", render.BufferUsageFlagVertex)
				inventory.AcquireBuffer("missing.bin", render.BufferUsageFlagVertex)
			}
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				inventory.AcquireBuffer("vanishing.bin", render.BufferUsageFlagVertex)
				inventory.AcquireTexture("vanishing.png", render.TextureOptions{})
			}
		},
		onRecovered: recordCallCount(h, recoveredCallCounts),
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Step()
	reportCountBeforeTheLoss := len(h.Reports())
	for _, name := range []string{"generated.png", "generated.bin", "vanishing.bin", "vanishing.png"} {
		if err := files.Remove(name); err != nil {
			t.Fatal(err)
		}
	}

	recoveryCallIndex, _ := loseAndRecover(t, h, recoveredCallCounts)
	h.Step()
	h.Step()

	if reportCountBeforeTheLoss != 2 {
		t.Errorf("render reported %d failed loads before the loss, want 2, the missing texture and buffer", reportCountBeforeTheLoss)
	}
	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindLoadFailed}, 6); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: the missing texture and buffer once, before the loss, the vanishing buffer and texture once each, in the frame lost, and the texture and the buffer whose bytes were removed once each after it", kinds, want)
	}
	textureCalls := readTextureCalls(h.Calls())
	for _, want := range []struct {
		what    string
		texture render.Texture
		texels  int
	}{
		{what: "whose bytes were removed", texture: generated, texels: 4},
		{what: "that failed before the loss", texture: missing, texels: 1},
	} {
		magenta := slices.Repeat([]byte{255, 0, 255, 255}, want.texels)
		if !slices.ContainsFunc(textureCalls.find(recording.CallKindWriteTexture, want.texture), func(write assetCall[render.Texture]) bool {
			return write.index > recoveryCallIndex && bytes.Equal(write.arguments.(recording.WriteTextureArguments).Data, magenta)
		}) {
			t.Errorf("the texture %s was not written magenta after the recovery", want.what)
		}
	}
}
