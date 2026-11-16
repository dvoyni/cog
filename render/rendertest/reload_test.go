package rendertest_test

import (
	"bytes"
	"io/fs"
	"slices"
	"sync"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// openCountingFS is a filesystem that counts the opens of each file of the
// one it wraps, a read among them, and stats its files without opening
// them.
type openCountingFS struct {
	files      fs.FS
	mutex      sync.Mutex
	openCounts map[string]int
}

func (f *openCountingFS) Open(name string) (fs.File, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.openCounts == nil {
		f.openCounts = map[string]int{}
	}
	f.openCounts[name]++
	return f.files.Open(name)
}

func (f *openCountingFS) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(f.files, name)
}

// countOpens answers how many times name was opened.
func (f *openCountingFS) countOpens(name string) int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.openCounts[name]
}

// assetWrite is a write of a texture or buffer: its data, and how many
// presents came before it.
type assetWrite struct {
	data         []byte
	presentCount int
}

// equal answers whether w and other wrote the same data after as many
// presents.
func (w assetWrite) equal(other assetWrite) bool {
	return bytes.Equal(w.data, other.data) && w.presentCount == other.presentCount
}

// findWrites answers the writes of kind on asset among calls.
func findWrites[H comparable](calls assetCallSequence[H], kind recording.CallKind, asset H) []assetWrite {
	var writes []assetWrite
	for _, call := range calls.find(kind, asset) {
		data, _ := readWrittenData(call.arguments)
		writes = append(writes, assetWrite{data: data, presentCount: call.presentCount})
	}
	return writes
}

func TestReloadChangedWritesAChangedFileUnderItsHandleAtTheNextFramesStartAndReadsNoUnchangedFile(t *testing.T) {
	h := rendertest.NewHarness(t)
	albedoPNG, albedoTexels := makePNG(t, 4, 4, 255)
	newAlbedoPNG, newAlbedoTexels := makePNG(t, 4, 4, 128)
	stillPNG, stillTexels := makePNG(t, 2, 2, 255)
	memory := filesystems.NewMemoryFS()
	putFile(t, memory, "albedo.png", albedoPNG)
	putFile(t, memory, "still.png", stillPNG)
	putFile(t, memory, "crate.vertices", []byte{1, 2, 3, 4, 5, 6, 7, 8})
	putFile(t, memory, "still.vertices", []byte{9, 9, 9, 9})
	files := &openCountingFS{files: memory}
	var albedo, still render.Texture
	var vertices, stillVertices render.Buffer
	var stillOpenCounts [2]int
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				albedo = inventory.AcquireTexture("albedo.png", render.TextureOptions{Mipmaps: true})
				still = inventory.AcquireTexture("still.png", render.TextureOptions{})
				vertices = inventory.AcquireBuffer("crate.vertices", render.BufferUsageFlagVertex)
				stillVertices = inventory.AcquireBuffer("still.vertices", render.BufferUsageFlagVertex)
			case 1:
				putFile(t, memory, "albedo.png", newAlbedoPNG)
				putFile(t, memory, "crate.vertices", []byte{8, 7, 6, 5, 4, 3, 2, 1})
				openCounts := [2]int{files.countOpens("still.png"), files.countOpens("still.vertices")}
				inventory.ReloadChanged()
				stillOpenCounts = [2]int{files.countOpens("still.png") - openCounts[0], files.countOpens("still.vertices") - openCounts[1]}
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	calls := h.Calls()
	textureCalls := readTextureCalls(calls)
	bufferCalls := readBufferCalls(calls)
	if stillOpenCounts != [2]int{} {
		t.Errorf("ReloadChanged opened the unchanged still.png and still.vertices %v times, want none", stillOpenCounts)
	}
	for _, want := range []struct {
		what         string
		createdCount int
		writes       []assetWrite
		want         []assetWrite
	}{
		{what: "the changed texture", createdCount: len(textureCalls.find(recording.CallKindCreateTexture, albedo)), writes: findWrites(textureCalls, recording.CallKindWriteTexture, albedo), want: []assetWrite{{albedoTexels, 0}, {newAlbedoTexels, 2}}},
		{what: "the changed buffer", createdCount: len(bufferCalls.find(recording.CallKindCreateBuffer, vertices)), writes: findWrites(bufferCalls, recording.CallKindWriteBuffer, vertices), want: []assetWrite{{[]byte{1, 2, 3, 4, 5, 6, 7, 8}, 0}, {[]byte{8, 7, 6, 5, 4, 3, 2, 1}, 2}}},
		{what: "the unchanged texture", createdCount: len(textureCalls.find(recording.CallKindCreateTexture, still)), writes: findWrites(textureCalls, recording.CallKindWriteTexture, still), want: []assetWrite{{stillTexels, 0}}},
		{what: "the unchanged buffer", createdCount: len(bufferCalls.find(recording.CallKindCreateBuffer, stillVertices)), writes: findWrites(bufferCalls, recording.CallKindWriteBuffer, stillVertices), want: []assetWrite{{[]byte{9, 9, 9, 9}, 0}}},
	} {
		if want.createdCount != 1 || !slices.EqualFunc(want.writes, want.want, assetWrite.equal) {
			t.Errorf("%s was made %d times and written %v, want made once and written %v: reloaded while the second frame recorded, it is written in place at the third's start, the second drawing it old", want.what, want.createdCount, want.writes, want.want)
		}
	}
	frames := h.RecordedFrames()
	buffers := readSubmittedBuffers(frames[2])
	if len(buffers) == 0 {
		t.Fatal("the third frame submitted no command list")
	}
	viewLevels, groupLevels := readTextureLevels(calls)
	wantMips := []mipPass{{target: textureLevel{albedo, 1, 0}, source: textureLevel{albedo, 0, 0}}, {target: textureLevel{albedo, 2, 0}, source: textureLevel{albedo, 1, 0}}}
	if got := readMipPasses(buffers[0], viewLevels, groupLevels); !slices.Equal(got, wantMips) {
		t.Errorf("the third frame's first list, render's own, drew %v, want the reloaded texture's mips, %v", got, wantMips)
	}
}

func TestATextureReloadedAtAnotherSizeGetsANewGfxTextureItsMaterialsRebindToOrUnbindAViewItLacksAndTheOldIsFreedAtTheNextFramesStart(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindParameterRefused)
	albedoPNG, _ := makePNG(t, 4, 4, 255)
	widePNG, wideTexels := makePNG(t, 8, 2, 255)
	tinyPNG, _ := makePNG(t, 1, 1, 255)
	files := makeDrawFiles(t)
	putFile(t, files, "material.wgsl", []byte("// material.wgsl"))
	putFile(t, files, "material.wgsl.json", []byte(materialDeclaration))
	putFile(t, files, "albedo.png", albedoPNG)
	putFile(t, files, "detail.png", albedoPNG)
	var scene parameterScene
	var albedo, detail render.Texture
	var detailed render.Material
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex == 0 {
				scene = makeParameterScene(inventory)
				albedo = inventory.AcquireTexture("albedo.png", render.TextureOptions{Mipmaps: true})
				detail = inventory.AcquireTexture("detail.png", render.TextureOptions{Mipmaps: true})
				inventory.SetMaterialTexture(scene.material, "albedo", albedo)
				detailed = inventory.MakeMaterial(render.MaterialDescriptor{Label: "detailed", Shader: scene.shader, FragmentEntryPoint: "fragment_main"})
				inventory.SetMaterialTextureView(detailed, "albedo", render.TextureView{Texture: detail, BaseMipLevel: 1})
			}
			recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
				pass.SetMaterial(scene.material)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
				pass.SetMaterial(detailed)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			})
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				putFile(t, files, "albedo.png", widePNG)
				putFile(t, files, "detail.png", tinyPNG)
				inventory.ReloadChanged()
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	calls := h.Calls()
	textureCalls := readTextureCalls(calls)
	created := textureCalls.find(recording.CallKindCreateTexture, albedo)
	if len(created) != 2 {
		t.Fatalf("the texture was made %d times, want twice: at its acquire and at its reload", len(created))
	}
	if descriptor := created[1].arguments.(recording.CreateTextureArguments).Descriptor; descriptor.Size != (gfx.Extent3D{Width: 8, Height: 2, DepthOrArrayLayers: 1}) || descriptor.MipLevelCount != 4 {
		t.Errorf("the reload made a texture %+v with %d mip levels, want 8×2 with 4", descriptor.Size, descriptor.MipLevelCount)
	}
	releases := textureCalls.find(recording.CallKindReleaseTexture, albedo)
	submits := findCallIndices(calls, recording.CallKindSubmit)
	if len(releases) != 1 || releases[0].presentCount != 1 || len(submits) < 2 || releases[0].index > submits[1] {
		t.Errorf("the replaced texture was released %d times, the first after %d presents, want once, after the first frame's present and before the second frame's submit", len(releases), releases[0].presentCount)
	}
	if writes := findWrites(textureCalls, recording.CallKindWriteTexture, albedo); len(writes) != 2 || !writes[1].equal(assetWrite{wideTexels, 1}) {
		t.Errorf("the texture was written %v, want twice, the second its new texels in the second frame", writes)
	}
	draws := findPassDraws(h, "scene")
	if len(draws) != 2 || len(draws[0]) != 2 || len(draws[1]) != 2 {
		t.Fatalf("the frames drew %v, want two draws each", draws)
	}
	for frameIndex, mipLevelCount := range []uint32{3, 4} {
		parameter, _ := findParameter(draws[frameIndex][0], "albedo")
		if want := (render.TextureView{Texture: albedo, MipLevelCount: mipLevelCount, LayerCount: 1, Dimension: render.TextureViewDimension2D, Aspect: render.TextureAspectAll}); parameter.TextureView != want {
			t.Errorf("frame %d's draw bound albedo as %+v, want %+v", frameIndex, parameter.TextureView, want)
		}
	}
	for frameIndex, wantDefault := range []bool{false, true} {
		if parameter, _ := findParameter(draws[frameIndex][1], "albedo"); parameter.Default != wantDefault {
			t.Errorf("frame %d's draw of the detailed material bound albedo as %+v, want its default: %t, its view's mip levels gone after the reload", frameIndex, parameter, wantDefault)
		}
	}
	if refused := countReports(h, render.ReportKindParameterRefused); refused != 1 {
		t.Errorf("render reported %d refused values, want one: the detailed material's view", refused)
	}
}

func TestABufferReloadedAtAnotherSizeGetsANewGfxBufferAMeshWhoseRangeNoLongerFitsSkipsItsDrawsReportedOnceAndSoDoesAMaterialValue(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindDrawSkipped)
	h.ExpectReport(render.ReportKindParameterRefused)
	files := makeDrawFiles(t)
	putFile(t, files, "material.wgsl", []byte("// material.wgsl"))
	putFile(t, files, "material.wgsl.json", []byte(materialDeclaration))
	putFile(t, files, "quad.vertices", make([]byte, 48))
	putFile(t, files, "lights.bin", make([]byte, 32))
	var scene parameterScene
	var vertices, lights render.Buffer
	var mesh render.Mesh
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex == 0 {
				scene = makeParameterScene(inventory)
				vertices = inventory.AcquireBuffer("quad.vertices", render.BufferUsageFlagVertex)
				lights = inventory.AcquireBuffer("lights.bin", render.BufferUsageFlagStorage)
				mesh = inventory.MakeMesh(render.MeshDescriptor{Label: "quad", VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: vertices, Size: 48}, Layout: positionLayout}}})
				inventory.SetMaterialBuffer(scene.material, "lights", render.BufferRange{Buffer: lights, Size: 32})
			}
			recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
				pass.SetMaterial(scene.material)
				pass.Draw(mesh, render.DrawArgs{})
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			})
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			switch frameIndex {
			case 0:
				putFile(t, files, "quad.vertices", make([]byte, 24))
				putFile(t, files, "lights.bin", make([]byte, 16))
				inventory.ReloadChanged()
			case 2:
				putFile(t, files, "quad.vertices", make([]byte, 60))
				inventory.ReloadChanged()
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 4 {
		h.Step()
	}

	bufferCalls := readBufferCalls(h.Calls())
	for _, want := range []struct {
		what   string
		buffer render.Buffer
		sizes  []uint64
	}{
		{what: "the vertex buffer", buffer: vertices, sizes: []uint64{48, 24, 60}},
		{what: "the lights", buffer: lights, sizes: []uint64{32, 16}},
	} {
		var sizes []uint64
		for _, call := range bufferCalls.find(recording.CallKindCreateBuffer, want.buffer) {
			sizes = append(sizes, call.arguments.(recording.CreateBufferArguments).Descriptor.Size)
		}
		if !slices.Equal(sizes, want.sizes) {
			t.Errorf("%s was made at sizes %v, want %v", want.what, sizes, want.sizes)
		}
		if released := len(bufferCalls.find(recording.CallKindReleaseBuffer, want.buffer)); released != len(want.sizes)-1 {
			t.Errorf("%s's replaced buffers were released %d times, want %d", want.what, released, len(want.sizes)-1)
		}
	}
	var drawCounts []int
	var lightsDefaults []bool
	for _, draws := range findPassDraws(h, "scene") {
		drawCounts = append(drawCounts, len(draws))
		parameter, _ := findParameter(draws[len(draws)-1], "lights")
		lightsDefaults = append(lightsDefaults, parameter.Default)
	}
	if want := []int{2, 1, 1, 2}; !slices.Equal(drawCounts, want) {
		t.Errorf("the frames drew %v draws, want %v: the mesh's draws skipped while its range ends past its buffer", drawCounts, want)
	}
	if want := []bool{false, true, true, true}; !slices.Equal(lightsDefaults, want) {
		t.Errorf("the material's lights drew their default %v, want %v: unbound once their range ends past their buffer", lightsDefaults, want)
	}
	if skipped, refused := countReports(h, render.ReportKindDrawSkipped), countReports(h, render.ReportKindParameterRefused); skipped != 1 || refused != 1 {
		t.Errorf("render reported %d skipped draws and %d refused values, want one each", skipped, refused)
	}
}

func TestAFailedReloadKeepsWhatItsAssetDrewReportedOnceAndTheNextChangeRetries(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	albedoPNG, albedoTexels := makePNG(t, 4, 4, 255)
	fixedPNG, fixedTexels := makePNG(t, 4, 4, 128)
	files := filesystems.NewMemoryFS()
	putFile(t, files, "albedo.png", albedoPNG)
	putFile(t, files, "crate.vertices", []byte{1, 2, 3, 4})
	var albedo render.Texture
	var vertices render.Buffer
	var reportCounts []int
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			if frameIndex == 0 {
				albedo = inventory.AcquireTexture("albedo.png", render.TextureOptions{})
				vertices = inventory.AcquireBuffer("crate.vertices", render.BufferUsageFlagVertex)
			}
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			switch frameIndex {
			case 0:
				putFile(t, files, "albedo.png", []byte("not a picture"))
				putFile(t, files, "crate.vertices", nil)
				inventory.ReloadChanged()
				reportCounts = append(reportCounts, len(h.Reports()))
				inventory.ReloadChanged()
				reportCounts = append(reportCounts, len(h.Reports()))
			case 1:
				putFile(t, files, "albedo.png", fixedPNG)
				putFile(t, files, "crate.vertices", []byte{5, 6, 7, 8})
				inventory.ReloadChanged()
				reportCounts = append(reportCounts, len(h.Reports()))
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	if want := []int{2, 2, 2}; !slices.Equal(reportCounts, want) {
		t.Errorf("render had reported %v after each ReloadChanged, want %v: each broken file once, not again while unchanged, nor once fixed", reportCounts, want)
	}
	if kinds := makeReportKinds(h); !slices.Equal(kinds, []render.ReportKind{render.ReportKindLoadFailed, render.ReportKindLoadFailed}) {
		t.Errorf("render reported kinds %v, want two failed loads", kinds)
	}
	textureCalls := readTextureCalls(h.Calls())
	bufferCalls := readBufferCalls(h.Calls())
	for _, want := range []struct {
		what         string
		createdCount int
		writes       []assetWrite
		want         []assetWrite
	}{
		{what: "the texture", createdCount: len(textureCalls.find(recording.CallKindCreateTexture, albedo)), writes: findWrites(textureCalls, recording.CallKindWriteTexture, albedo), want: []assetWrite{{albedoTexels, 0}, {fixedTexels, 2}}},
		{what: "the buffer", createdCount: len(bufferCalls.find(recording.CallKindCreateBuffer, vertices)), writes: findWrites(bufferCalls, recording.CallKindWriteBuffer, vertices), want: []assetWrite{{[]byte{1, 2, 3, 4}, 0}, {[]byte{5, 6, 7, 8}, 2}}},
	} {
		if want.createdCount != 1 || !slices.EqualFunc(want.writes, want.want, assetWrite.equal) {
			t.Errorf("%s was made %d times and written %v, want made once and written %v: its broken file never, its fixed one in the third frame", want.what, want.createdCount, want.writes, want.want)
		}
	}
}

func TestABindGroupOfPassParametersNamingATextureReloadedAtAnotherSizeIsReleasedWhenItsReplacementIsSwappedIn(t *testing.T) {
	h := rendertest.NewHarness(t)
	environmentPNG, _ := makePNG(t, 4, 4, 255)
	widePNG, _ := makePNG(t, 8, 2, 255)
	files := makeDrawFiles(t)
	putFile(t, files, "parameters.wgsl", []byte("// parameters.wgsl"))
	putFile(t, files, "parameters.wgsl.json", []byte(parametersDeclaration))
	putFile(t, files, "environment.png", environmentPNG)
	var scene passParameterScene
	var environment render.Texture
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex == 0 {
				scene = makePassParameterScene(inventory)
				environment = inventory.AcquireTexture("environment.png", render.TextureOptions{})
			}
			recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
				pass.SetPassTexture("environment", environment)
				pass.SetMaterial(scene.material)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			})
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				putFile(t, files, "environment.png", widePNG)
				inventory.ReloadChanged()
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	calls := h.Calls()
	created := findCreatedBindGroups(calls, parameterBindGroupLabel)
	if len(created) != 2 {
		t.Fatalf("render made %d bind groups of pass parameters, want two: the first frame's and the second's, naming the new texture", len(created))
	}
	for group, frameIndex := range created {
		if frameIndex != 0 {
			continue
		}
		if releaseFrame, ok := findReleaseFrame(calls, recording.CallKindReleaseBindGroup, group); !ok || releaseFrame != 1 {
			t.Errorf("the first frame's bind group was released after %d submits (%t), want after 1: at the second frame's start, when the reload was swapped in", releaseFrame, ok)
		}
	}
}

func TestATextureOrBufferWhoseLoadFailedIsWrittenFromItsFileAfterADeviceLossOnceItReloaded(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	recoveredCallCounts := make(chan int, 1)
	latePNG, lateTexels := makePNG(t, 2, 2, 255)
	files := filesystems.NewMemoryFS()
	putFile(t, files, "late.vertices", []byte{1, 2, 3, 4})
	// late is the texture of late.png, missing when acquired, and
	// lateVertices the buffer of late.vertices, put again at another size
	// after its acquire and before its bytes were written.
	var late render.Texture
	var lateVertices render.Buffer
	h.Spawn(nil, &loaderPlugin{
		files: files,
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			switch frameIndex {
			case 0:
				late = inventory.AcquireTexture("late.png", render.TextureOptions{})
				lateVertices = inventory.AcquireBuffer("late.vertices", render.BufferUsageFlagVertex)
				putFile(t, files, "late.vertices", []byte{1, 2, 3, 4, 5, 6, 7, 8})
			case 1:
				putFile(t, files, "late.png", latePNG)
				inventory.ReloadChanged()
			}
		},
		onRecovered: recordCallCount(h, recoveredCallCounts),
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	for range 3 {
		h.Step()
	}

	recoveryCallIndex, callCountAtRecovered := loseAndRecover(t, h, recoveredCallCounts)

	calls := h.Calls()
	for _, want := range []struct {
		what    string
		written bool
	}{
		{what: "the texture", written: containsAssetWriteBetween(readTextureCalls(calls).find(recording.CallKindWriteTexture, late), recoveryCallIndex, callCountAtRecovered)},
		{what: "the buffer", written: containsAssetWriteBetween(readBufferCalls(calls).find(recording.CallKindWriteBuffer, lateVertices), recoveryCallIndex, callCountAtRecovered)},
	} {
		if !want.written {
			t.Errorf("%s was not written again between the recovery, at call %d, and RecoveredEvent, at %d: its load failed, but its reload did not", want.what, recoveryCallIndex, callCountAtRecovered)
		}
	}
	if recoveryCalls := calls[recoveryCallIndex:callCountAtRecovered]; !containsWrite(recoveryCalls, lateTexels) || !containsWrite(recoveryCalls, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("the recovery did not write the texture's and the buffer's reloaded files")
	}
}
