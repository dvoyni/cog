package rendertest_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

func TestAnAcquiredTextureIsMadeFromItsHeaderAtOnceAndItsBodyWrittenBeforeTheSubmitOfTheFrameItWasAcquiredBeforeOrDuring(t *testing.T) {
	h := rendertest.NewHarness(t)
	albedo, albedoTexels := makePNG(t, 5, 3, 255)
	mask, maskTexels := makePNG(t, 6, 4, 128)
	palette, paletteTexels := makePalettedPNG(t)
	// acquired are the textures acquired during the first frame's recording,
	// after its end, and during it again, createdAtOnce whether each was made
	// when its acquire returned, and writtenWhileRecording whether the second
	// was written while the frame after it recorded.
	var acquired [3]render.Texture
	var createdAtOnce [3]bool
	var writtenWhileRecording bool
	h.Spawn(nil, &loaderPlugin{
		files: fstest.MapFS{"albedo.png": {Data: albedo}, "textures/mask.png": {Data: mask}, "palette.png": {Data: palette}},
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				acquired[0] = inventory.AcquireTexture("albedo.png", render.TextureOptions{ColorSpace: render.ColorSpaceSRGB})
				_, createdAtOnce[0] = readTextureCalls(h.Calls()).findCreated[recording.CreateTextureArguments](acquired[0])
				acquired[2] = inventory.AcquireTexture("palette.png", render.TextureOptions{ColorSpace: render.ColorSpaceLinear})
				_, createdAtOnce[2] = readTextureCalls(h.Calls()).findCreated[recording.CreateTextureArguments](acquired[2])
			case 1:
				writtenWhileRecording = awaitTextureWrite(h, acquired[1])
			}
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				acquired[1] = inventory.AcquireTexture("textures/mask.png", render.TextureOptions{ColorSpace: render.ColorSpaceLinear, Mipmaps: true})
				_, createdAtOnce[1] = readTextureCalls(h.Calls()).findCreated[recording.CreateTextureArguments](acquired[1])
			}
		},
	})

	h.Step()
	h.Step()

	calls := h.Calls()
	textureCalls := readTextureCalls(calls)
	submits := findCallIndices(calls, recording.CallKindSubmit)
	if !writtenWhileRecording {
		t.Errorf("the texture acquired before a frame was not written while the frame recorded")
	}
	for index, want := range []struct {
		descriptor  gfx.TextureDescriptor
		texels      []byte
		submitIndex int
		submit      string
	}{
		{descriptor: gfx.TextureDescriptor{Size: gfx.Extent3D{Width: 5, Height: 3, DepthOrArrayLayers: 1}, MipLevelCount: 1, Format: gfx.TextureFormatRGBA8UnormSrgb}, texels: albedoTexels, submitIndex: 0, submit: "the submit of the frame it was acquired during"},
		{descriptor: gfx.TextureDescriptor{Size: gfx.Extent3D{Width: 6, Height: 4, DepthOrArrayLayers: 1}, MipLevelCount: 3, Format: gfx.TextureFormatRGBA8Unorm}, texels: maskTexels, submitIndex: 1, submit: "the submit of the frame after it was acquired"},
		{descriptor: gfx.TextureDescriptor{Size: gfx.Extent3D{Width: 2, Height: 2, DepthOrArrayLayers: 1}, MipLevelCount: 1, Format: gfx.TextureFormatRGBA8Unorm}, texels: paletteTexels, submitIndex: 0, submit: "the submit of the frame it was acquired during"},
	} {
		if !createdAtOnce[index] {
			t.Errorf("texture %d was not made when its acquire returned", index)
		}
		created, ok := textureCalls.findCreated[recording.CreateTextureArguments](acquired[index])
		if !ok {
			t.Errorf("texture %d, %v, was not made once", index, acquired[index])
			continue
		}
		if got := created.Descriptor; got.Size != want.descriptor.Size || got.MipLevelCount != want.descriptor.MipLevelCount || got.Format != want.descriptor.Format {
			t.Errorf("texture %d was made %+v with %d mip levels in format %d, want %+v with %d in format %d", index, got.Size, got.MipLevelCount, got.Format, want.descriptor.Size, want.descriptor.MipLevelCount, want.descriptor.Format)
		}
		writes := textureCalls.find(recording.CallKindWriteTexture, acquired[index])
		if len(writes) != 1 {
			t.Errorf("texture %d was written %d times, want once", index, len(writes))
			continue
		}
		write := writes[0].arguments.(recording.WriteTextureArguments)
		wantLayout := gfx.ImageDataLayout{BytesPerRow: 4 * want.descriptor.Size.Width, RowsPerImage: want.descriptor.Size.Height}
		if write.Destination.MipLevel != 0 || write.Layout != wantLayout || write.Size != want.descriptor.Size || !bytes.Equal(write.Data, want.texels) {
			t.Errorf("texture %d was written at mip level %d, laid out as %+v, %+v of %v, want its mip level 0, laid out as %+v, %+v of %v", index, write.Destination.MipLevel, write.Layout, write.Size, write.Data, wantLayout, want.descriptor.Size, want.texels)
		}
		if want.submitIndex >= len(submits) || writes[0].index > submits[want.submitIndex] {
			t.Errorf("texture %d was written at call %d, after %s, at %v", index, writes[0].index, want.submit, submits)
		}
	}
}

func TestASecondAcquireOfAKeySharesItsTextureAndTheLastReleaseFreesItAtTheEndOfThatFrameUnlessRevived(t *testing.T) {
	h := rendertest.NewHarness(t)
	albedo, _ := makePNG(t, 4, 4, 255)
	sRGB := render.TextureOptions{ColorSpace: render.ColorSpaceSRGB}
	// first and second are the first frame's acquires of albedo.png in sRGB,
	// revived the second frame's, after the last of those was released, and
	// reacquired the fourth frame's, after it was freed; linear and mipmapped
	// are its acquires with other options, released in the first frame.
	var first, second, revived, reacquired, linear, mipmapped render.Texture
	h.Spawn(nil, &loaderPlugin{
		files: fstest.MapFS{"albedo.png": {Data: albedo}},
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				first = inventory.AcquireTexture("albedo.png", sRGB)
				second = inventory.AcquireTexture("albedo.png", sRGB)
				linear = inventory.AcquireTexture("albedo.png", render.TextureOptions{ColorSpace: render.ColorSpaceLinear})
				mipmapped = inventory.AcquireTexture("albedo.png", render.TextureOptions{ColorSpace: render.ColorSpaceSRGB, Mipmaps: true})
				inventory.ReleaseTexture(linear)
				inventory.ReleaseTexture(mipmapped)
				inventory.ReleaseTexture(first)
			case 1:
				inventory.ReleaseTexture(second)
				revived = inventory.AcquireTexture("albedo.png", sRGB)
			case 2:
				inventory.ReleaseTexture(revived)
			case 3:
				reacquired = inventory.AcquireTexture("albedo.png", sRGB)
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 4 {
		h.Step()
	}

	if second != first || revived != first {
		t.Errorf("acquires of one key before its texture was freed answered %v and %v, want %v each", second, revived, first)
	}
	if linear == first || mipmapped == first || linear == mipmapped {
		t.Errorf("acquires of the file in another colour space and with mipmaps answered %v and %v, want textures of their own, not %v", linear, mipmapped, first)
	}
	if reacquired == first {
		t.Errorf("an acquire after the texture was freed answered %v, the freed handle, want another", first)
	}
	textureCalls := readTextureCalls(h.Calls())
	for _, want := range []struct {
		texture    render.Texture
		frameIndex int
		what       string
	}{
		{texture: linear, frameIndex: 0, what: "a texture released once, in the first frame,"},
		{texture: mipmapped, frameIndex: 0, what: "a texture released once, in the first frame,"},
		{texture: first, frameIndex: 2, what: "a texture acquired twice, released in the first and second frames, acquired again in the second and released in the third,"},
	} {
		if frameIndex, ok := textureCalls.findFreeingFrame(want.texture); !ok || frameIndex != want.frameIndex {
			t.Errorf("%s was freed: %v, after the present of frame %d, want freed between the present of frame %d and the next frame's acquire", want.what, ok, frameIndex, want.frameIndex)
		}
	}
	if _, ok := textureCalls.findCreated[recording.CreateTextureArguments](reacquired); !ok {
		t.Errorf("the texture acquired after the first was freed, %v, was not made once", reacquired)
	}
}

func TestParallelAcquiresOfOneKeyShareOneTextureFreedOnceByTheirReleases(t *testing.T) {
	const acquireCount = 64
	h := rendertest.NewHarness(t)
	albedo, _ := makePNG(t, 4, 4, 255)
	// mutex guards acquired, every texture the tasks acquired.
	var mutex sync.Mutex
	var acquired []render.Texture
	h.Spawn(nil, &loaderPlugin{
		files:              &overlappingFS{FS: fstest.MapFS{"albedo.png": {Data: albedo}}, name: "albedo.png"},
		recordingTaskCount: 2,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			for range acquireCount {
				switch frameIndex {
				case 0:
					texture := inventory.AcquireTexture("albedo.png", render.TextureOptions{})
					mutex.Lock()
					acquired = append(acquired, texture)
					mutex.Unlock()
				case 1:
					mutex.Lock()
					texture := acquired[0]
					mutex.Unlock()
					inventory.ReleaseTexture(texture)
				}
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()
	h.Step()

	mutex.Lock()
	defer mutex.Unlock()
	if len(acquired) != 2*acquireCount || slices.ContainsFunc(acquired, func(texture render.Texture) bool { return texture != acquired[0] }) {
		t.Fatalf("two tasks acquiring one key %d times each got %v, want one texture every time", acquireCount, acquired)
	}
	calls := h.Calls()
	if created := findCallIndices(calls, recording.CallKindCreateTexture); len(created) != 1 {
		t.Fatalf("render made %d textures, want one", len(created))
	}
	if frameIndex, ok := readTextureCalls(calls).findFreeingFrame(acquired[0]); !ok || frameIndex != 1 {
		t.Errorf("the texture was freed: %v, after the present of frame %d, want freed at the end of the frame of its last release, 1", ok, frameIndex)
	}
}

func TestAHeaderFailureGetsAOneByOneMagentaTextureAndABodyFailureMagentaAtItsSizeEachReportedOncePerKeyUntilFreed(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	encoded, _ := makePNG(t, 4, 2, 255)
	resized, _ := makePNG(t, 3, 3, 255)
	files := filesystems.NewMemoryFS()
	// broken.png is a PNG whose header is whole and whose body is cut short,
	// and resized.png one put again at another size between its acquire and
	// the decode of its body.
	putFile(t, files, "broken.png", encoded[:len(encoded)-20])
	putFile(t, files, "resized.png", encoded)
	// missingTexture is the first frame's texture of missing.png,
	// brokenTexture that of broken.png, and resizedTexture that of
	// resized.png, acquired after the first frame.
	var missingTexture, brokenTexture, resizedTexture render.Texture
	var reportCountAfterTheFirstAcquire int
	h.Spawn(nil, &loaderPlugin{
		files: files,
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				resizedTexture = inventory.AcquireTexture("resized.png", render.TextureOptions{})
			}
		},
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				missingTexture = inventory.AcquireTexture("missing.png", render.TextureOptions{})
				reportCountAfterTheFirstAcquire = len(h.Reports())
				inventory.ReleaseTexture(inventory.AcquireTexture("missing.png", render.TextureOptions{}))
				inventory.ReleaseTexture(missingTexture)
				brokenTexture = inventory.AcquireTexture("broken.png", render.TextureOptions{ColorSpace: render.ColorSpaceLinear})
			case 1:
				inventory.ReleaseTexture(inventory.AcquireTexture("missing.png", render.TextureOptions{}))
			}
		},
	})

	h.Step()
	putFile(t, files, "resized.png", resized)
	h.Step()

	if reportCountAfterTheFirstAcquire != 1 {
		t.Errorf("render had logged %d reports when the acquire of a missing file returned, want 1", reportCountAfterTheFirstAcquire)
	}
	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindLoadFailed}, 4); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: the missing file once in each frame, as it was freed between them, and the broken and resized ones once each", kinds, want)
	}
	textureCalls := readTextureCalls(h.Calls())
	for _, want := range []struct {
		texture render.Texture
		size    gfx.Extent3D
		format  gfx.TextureFormat
		what    string
	}{
		{texture: missingTexture, size: gfx.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1}, format: gfx.TextureFormatRGBA8UnormSrgb, what: "the missing file"},
		{texture: brokenTexture, size: gfx.Extent3D{Width: 4, Height: 2, DepthOrArrayLayers: 1}, format: gfx.TextureFormatRGBA8Unorm, what: "the file with a broken body"},
		{texture: resizedTexture, size: gfx.Extent3D{Width: 4, Height: 2, DepthOrArrayLayers: 1}, format: gfx.TextureFormatRGBA8UnormSrgb, what: "the file resized after its header was read"},
	} {
		created, ok := textureCalls.findCreated[recording.CreateTextureArguments](want.texture)
		if !ok {
			t.Errorf("the texture of %s, %v, was not made once", want.what, want.texture)
			continue
		}
		if created.Descriptor.Size != want.size || created.Descriptor.Format != want.format || created.Descriptor.MipLevelCount != 1 {
			t.Errorf("the texture of %s was made %+v in format %d with %d mip levels, want %+v in format %d with 1", want.what, created.Descriptor.Size, created.Descriptor.Format, created.Descriptor.MipLevelCount, want.size, want.format)
		}
		magenta := slices.Repeat([]byte{255, 0, 255, 255}, int(want.size.Width*want.size.Height))
		writes := textureCalls.find(recording.CallKindWriteTexture, want.texture)
		if len(writes) != 1 {
			t.Errorf("the texture of %s was written %d times, want once", want.what, len(writes))
			continue
		}
		if write := writes[0].arguments.(recording.WriteTextureArguments); !bytes.Equal(write.Data, magenta) || write.Size != want.size {
			t.Errorf("the texture of %s was written %+v of %v, want %+v of magenta", want.what, write.Size, write.Data, want.size)
		}
	}
}

func TestAReleaseOfATextureNoAcquireHoldsIsRefusedAndReported(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindReleaseRefused)
	albedo, _ := makePNG(t, 4, 4, 255)
	h.Spawn(nil, &loaderPlugin{
		files: fstest.MapFS{"albedo.png": {Data: albedo}},
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex > 0 {
				return
			}
			loaded := inventory.AcquireTexture("albedo.png", render.TextureOptions{})
			inventory.ReleaseTexture(loaded)
			inventory.ReleaseTexture(loaded)
			inventory.ReleaseTexture(frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "bloom", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm}))
			inventory.ReleaseTexture(render.Texture{})
		},
	})

	h.Step()
	h.Step()

	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindReleaseRefused}, 3); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: a second release of a texture acquired once, a release of a temporary and of the zero Texture", kinds, want)
	}
}

// putFile puts data into files at name, failing the test when the put fails.
func putFile(t *testing.T, files *filesystems.MemoryFS, name string, data []byte) {
	t.Helper()
	if err := files.Put(name, data); err != nil {
		t.Fatal(err)
	}
}

// awaitTextureWrite waits up to a second for gfx to have written the texture
// behind texture, and answers whether it has.
func awaitTextureWrite(h *rendertest.Harness, texture render.Texture) bool {
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if len(readTextureCalls(h.Calls()).find(recording.CallKindWriteTexture, texture)) > 0 {
			return true
		}
	}
	return false
}

// makePNG answers a PNG of width by height texels, each a colour of its own
// with alpha, opaque when 255, and its texels in RGBA order, row by row, as
// render writes them.
func makePNG(t *testing.T, width, height int, alpha uint8) ([]byte, []byte) {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.SetNRGBA(x, y, color.NRGBA{R: uint8(40 * x), G: uint8(60 * y), B: uint8(x + y), A: alpha})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes(), picture.Pix
}

// makePalettedPNG answers a paletted PNG of 2 by 2 texels, opaque orange and
// fully transparent ones in turn, and its texels in RGBA order, row by row,
// as render writes them.
func makePalettedPNG(t *testing.T) ([]byte, []byte) {
	t.Helper()
	orange := color.NRGBA{R: 200, G: 100, B: 50, A: 255}
	picture := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{orange, color.NRGBA{}})
	picture.SetColorIndex(1, 0, 1)
	picture.SetColorIndex(0, 1, 1)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes(), []byte{200, 100, 50, 255, 0, 0, 0, 0, 0, 0, 0, 0, 200, 100, 50, 255}
}

// findCallIndices answers the index of every call of kind among calls.
func findCallIndices(calls []recording.Call, kind recording.CallKind) []int {
	var indices []int
	for index, call := range calls {
		if call.Kind == kind {
			indices = append(indices, index)
		}
	}
	return indices
}

// makeReportKinds answers the kind of each report h's kernel logged so far.
func makeReportKinds(h *rendertest.Harness) []render.ReportKind {
	var kinds []render.ReportKind
	for _, report := range h.Reports() {
		kinds = append(kinds, report.Kind)
	}
	return kinds
}
