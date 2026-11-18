package rendertest_test

import (
	"bytes"
	"image/color"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestGenerateMipsDrawsEveryLevelOfTheChosenLayersOfAMadeTextureWhereTheListCallsIt(t *testing.T) {
	h := rendertest.NewHarness(t)
	var array, cube render.Texture
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		array = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "atlas", Width: 8, Height: 4, LayerCount: 4, MipLevelCount: 3, Format: render.TextureFormatRGBA8UnormSrgb}, nil)
		cube = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "sky", Width: 4, Height: 4, LayerCount: 6, MipLevelCount: 2, Format: render.TextureFormatRGBA16Float}, nil)
		list := frame.BeginCommandList("mips", drawOrder)
		list.CopyTexture(array, render.TextureRegion{}, array, render.TextureRegion{Layer: 3})
		list.GenerateMips(array, 1, 2)
		// A zero layer count means every layer from the first: a cube's six
		// faces.
		list.GenerateMips(cube, 0, 0)
		list.CopyTexture(cube, render.TextureRegion{}, cube, render.TextureRegion{Layer: 5})
		list.End()
	})
	list := findInspectedList(h, "mips")
	var want []render.ColorTarget
	for mipLevel := uint32(1); mipLevel < 3; mipLevel++ {
		for _, layer := range []uint32{1, 2} {
			want = append(want, render.ColorTarget{Texture: array, MipLevel: mipLevel, Layer: layer, Clear: true})
		}
	}
	for layer := range uint32(6) {
		want = append(want, render.ColorTarget{Texture: cube, MipLevel: 1, Layer: layer, Clear: true})
	}
	var got []render.ColorTarget
	for index, pass := range list.Passes {
		got = append(got, pass.ColorTargets...)
		if wantPosition := index + 1; pass.Position != wantPosition {
			t.Errorf("mip pass %d is at position %d of the list, want %d, after its copy and before the next", index, pass.Position, wantPosition)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("the list drew into %v, want %v", got, want)
	}
	if copies := list.Copies; len(copies) != 2 || copies[1].Position != len(want)+1 {
		t.Errorf("the list's copies are %+v, want two, the second after the mips", copies)
	}
}

func TestListsGeneratingMipsAtOnceEachDrawTheirOwnTexturesLevels(t *testing.T) {
	const listCount = 4
	h := rendertest.NewHarness(t)
	// Each list's texture is of a format of its own, so the lists make the
	// mip shader and their pipelines at once.
	formats := [listCount]render.TextureFormat{render.TextureFormatRGBA8Unorm, render.TextureFormatRGBA16Float, render.TextureFormatBGRA8Unorm, render.TextureFormatR8Unorm}
	var textures [listCount]render.Texture
	// nextIndex hands each recording task a list and a texture of its own.
	var nextIndex atomic.Int32
	h.Spawn(nil, &loaderPlugin{files: makeDrawFiles(t), recordingTaskCount: listCount, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		index := int(nextIndex.Add(1)) - 1
		textures[index] = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "array", Width: 4, Height: 4, LayerCount: 4, MipLevelCount: 3, Format: formats[index]}, nil)
		list := frame.BeginCommandList("mips", drawOrder+render.Order(index))
		list.GenerateMips(textures[index], 0, 0)
		list.End()
	}})

	h.Step()

	lists := 0
	for _, list := range h.Frames()[0].CommandLists {
		if list.Label != "mips" {
			continue
		}
		lists++
		if len(list.Passes) != 8 {
			t.Errorf("a list drew %d mip passes, want 8: two levels of four layers", len(list.Passes))
			continue
		}
		texture := list.Passes[0].ColorTargets[0].Texture
		for _, pass := range list.Passes {
			if target := pass.ColorTargets[0].Texture; target != texture {
				t.Errorf("a list's mip pass drew into %v, want its own texture %v alone", target, texture)
			}
		}
	}
	if lists != listCount {
		t.Errorf("the reader read %d lists generating mips, want %d", lists, listCount)
	}
}

func TestGenerateMipsOfATextureItCannotDrawIsRefusedReportedAndRecordsNothing(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindMipsRefused)
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		makeMipped := func(format render.TextureFormat) render.Texture {
			return inventory.MakeTexture[byte](render.TextureDescriptor{Label: "mipped", Width: 4, Height: 4, LayerCount: 4, MipLevelCount: 3, Format: format}, nil)
		}
		array := makeMipped(render.TextureFormatRGBA8Unorm)
		single := inventory.MakeTexture[byte](render.TextureDescriptor{Label: "single", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm}, nil)
		list := frame.BeginCommandList("mips", drawOrder)
		// A texture render does not hold.
		list.GenerateMips(render.Texture{}, 0, 1)
		// Layers past the texture's last.
		list.GenerateMips(array, 3, 2)
		list.GenerateMips(array, 4, 0)
		// Formats a sampler cannot filter or a pass cannot draw into.
		list.GenerateMips(makeMipped(render.TextureFormatR32Float), 0, 0)
		list.GenerateMips(makeMipped(render.TextureFormatRGBA8Uint), 0, 0)
		list.GenerateMips(makeMipped(render.TextureFormatRGBA8Snorm), 0, 0)
		list.GenerateMips(makeMipped(render.TextureFormatDepth32Float), 0, 0)
		// A texture of one level has no mips to generate, and is no mistake.
		list.GenerateMips(single, 0, 0)
		list.End()
	})

	if count := countReports(h, render.ReportKindMipsRefused); count != 7 {
		t.Errorf("render reported %d refused mips, want seven", count)
	}
	if passes := findInspectedList(h, "mips").Passes; len(passes) != 0 {
		t.Errorf("the list recorded passes %+v, want none", passes)
	}
}

func TestGenerateMipsAveragesEachLevelOfTheChosenLayersAndLeavesTheOthersUntouched(t *testing.T) {
	h := rendertest.NewHarness(t)
	// Each channel is a multiple of 16, so every level's averages are whole,
	// and each layer's ramp is its own.
	layerTexels := make([][]byte, 3)
	for layer := range layerTexels {
		_, layerTexels[layer] = encodePNG(t, 4, 4, func(x, y int) color.NRGBA {
			return color.NRGBA{R: uint8(64 * x), G: uint8(64 * y), B: uint8(16 * ((x + 3*y + layer) % 4)), A: uint8(255 - 64*layer)}
		})
	}

	read := drawPixels(t, h, makePixelFiles(t, nil), func(inventory render.Inventory, list *render.CommandList) []render.Readback {
		texture := inventory.MakeTexture(render.TextureDescriptor{Label: "array", Width: 4, Height: 4, LayerCount: 3, MipLevelCount: 3, Format: render.TextureFormatRGBA8Unorm}, slices.Concat(layerTexels...))
		list.GenerateMips(texture, 1, 1)
		var readbacks []render.Readback
		for layer := range uint32(3) {
			for mipLevel := uint32(1); mipLevel < 3; mipLevel++ {
				readbacks = append(readbacks, list.ReadTexture(texture, render.TextureRegion{MipLevel: mipLevel, Layer: layer}))
			}
		}
		return readbacks
	})

	for layer := range 3 {
		levels := [][]byte{layerTexels[layer]}
		for side := 2; side >= 1; side /= 2 {
			levels = append(levels, averageTexels(levels[len(levels)-1], side))
		}
		for index, side := range []int{2, 1} {
			want := levels[index+1]
			if layer != 1 {
				want = make([]byte, side*side*4)
			}
			if got := readTexelRows(read[layer*2+index], side, side, 4); !bytes.Equal(got, want) {
				t.Errorf("layer %d's mip level %d holds %v, want %v", layer, index+1, got, want)
			}
		}
	}
}

func TestGenerateMipsFiltersAnSRGBTextureInLinear(t *testing.T) {
	h := rendertest.NewHarness(t)
	// Three of the four texels are white and one black: their linear average
	// is 0.75, which sRGB encodes as 225, where averaging the encoded values
	// would give 191.
	texels := []byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 0, 0, 0, 255}

	read := drawPixels(t, h, makePixelFiles(t, nil), func(inventory render.Inventory, list *render.CommandList) []render.Readback {
		texture := inventory.MakeTexture(render.TextureDescriptor{Label: "srgb", Width: 2, Height: 2, MipLevelCount: 2, Format: render.TextureFormatRGBA8UnormSrgb}, texels)
		list.GenerateMips(texture, 0, 0)
		return []render.Readback{list.ReadTexture(texture, render.TextureRegion{MipLevel: 1})}
	})

	got := readTexelRows(read[0], 1, 1, 4)
	for channel, want := range []int{225, 225, 225, 255} {
		if difference := int(got[channel]) - want; difference < -1 || difference > 1 {
			t.Errorf("mip level 1 holds %v, want %d in channel %d, within 1", got, want, channel)
		}
	}
}

// findInspectedList answers the command list labelled label of the first
// frame h's reader read.
func findInspectedList(h *rendertest.Harness, label string) render.InspectedCommandList {
	for _, list := range h.Frames()[0].CommandLists {
		if list.Label == label {
			return list
		}
	}
	return render.InspectedCommandList{}
}
