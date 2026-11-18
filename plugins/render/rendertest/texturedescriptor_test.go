package rendertest_test

import (
	"testing"

	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestTextureDescriptorAnswersAMadeOrLoadedTexturesDescriptorUntilItIsFreed(t *testing.T) {
	h := rendertest.NewHarness(t)
	files := makeDrawFiles(t)
	png, _ := makePNG(t, 8, 4, 255)
	putFile(t, files, "textures/stone.png", png)
	made := render.TextureDescriptor{Label: "atlas", Width: 16, Height: 8, LayerCount: 3, MipLevelCount: 2, Format: render.TextureFormatRGBA16Float}
	// answers are what each frame's TextureDescriptor answered of the made
	// texture, the loaded one and the zero Texture.
	var answers [3][3]descriptorAnswer
	var madeTexture, loadedTexture render.Texture
	var allocations float64
	h.Spawn(nil, &loaderPlugin{files: files, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0:
			madeTexture = inventory.MakeTexture[byte](made, nil)
			loadedTexture = inventory.AcquireTexture("textures/stone.png", render.TextureOptions{Mipmaps: true})
			allocations = testing.AllocsPerRun(heldCallCount, func() { inventory.TextureDescriptor(madeTexture) })
		case 1:
			inventory.ReleaseTexture(madeTexture)
			inventory.ReleaseTexture(loadedTexture)
		}
		for index, texture := range []render.Texture{madeTexture, loadedTexture, {}} {
			answer := &answers[frameIndex][index]
			answer.descriptor, answer.ok = inventory.TextureDescriptor(texture)
		}
	}})

	for range 3 {
		h.Step()
	}

	if allocations != 0 && !raceEnabled {
		t.Errorf("TextureDescriptor allocated %v times, want none", allocations)
	}
	madeFilled := made
	madeFilled.SampleCount = 1
	loaded := render.TextureDescriptor{Label: "textures/stone.png", Width: 8, Height: 4, LayerCount: 1, MipLevelCount: 4, SampleCount: 1, Format: render.TextureFormatRGBA8UnormSrgb}
	// Both textures are released in the second frame, and freed at its end.
	heldAnswers := [3]descriptorAnswer{{madeFilled, true}, {loaded, true}, {}}
	want := [3][3]descriptorAnswer{heldAnswers, heldAnswers, {}}
	for frameIndex := range answers {
		for index, got := range answers[frameIndex] {
			if got != want[frameIndex][index] {
				t.Errorf("frame %d: texture %d's descriptor is %+v, want %+v", frameIndex, index, got, want[frameIndex][index])
			}
		}
	}
}

// descriptorAnswer is what one call of TextureDescriptor answered.
type descriptorAnswer struct {
	descriptor render.TextureDescriptor
	ok         bool
}
