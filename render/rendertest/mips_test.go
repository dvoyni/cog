package rendertest_test

import (
	"os"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/gogpu/shader"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
	"github.com/dvoyni/cog/render/rendertest"
)

func TestATextureLoadedWithMipmapsGetsEveryLevelDrawnFromTheOneAboveInRendersOwnListSubmittedFirstInTheFrameItLoads(t *testing.T) {
	h := rendertest.NewHarness(t)
	mask, _ := makePNG(t, 6, 4, 255)
	albedo, _ := makePNG(t, 5, 3, 255)
	late, _ := makePNG(t, 8, 8, 255)
	dot, _ := makePNG(t, 1, 1, 255)
	// masked and plain are the textures acquired during the first frame's
	// recording, with Mipmaps and without, and delayed the one with Mipmaps
	// acquired after its end; the third frame acquires one of a single texel
	// with Mipmaps, which has no level to generate.
	var masked, plain, delayed render.Texture
	h.Spawn(nil, &loaderPlugin{
		files: fstest.MapFS{"mask.png": {Data: mask}, "albedo.png": {Data: albedo}, "late.png": {Data: late}, "dot.png": {Data: dot}},
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				masked = inventory.AcquireTexture("mask.png", render.TextureOptions{Mipmaps: true})
				plain = inventory.AcquireTexture("albedo.png", render.TextureOptions{})
			case 2:
				inventory.AcquireTexture("dot.png", render.TextureOptions{Mipmaps: true})
			}
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				delayed = inventory.AcquireTexture("late.png", render.TextureOptions{ColorSpace: render.ColorSpaceLinear, Mipmaps: true})
			}
		},
	})

	h.Step()
	h.Step()
	h.Step()

	frames := h.RecordedFrames()
	if len(frames) != 3 {
		t.Fatalf("the harness read back %d frames, want 3", len(frames))
	}
	calls := h.Calls()
	viewLevels, groupLevels := readTextureLevels(calls)
	for frameIndex, want := range [][]mipPass{
		{{target: textureLevel{masked, 1, 0}, source: textureLevel{masked, 0, 0}}, {target: textureLevel{masked, 2, 0}, source: textureLevel{masked, 1, 0}}},
		{{target: textureLevel{delayed, 1, 0}, source: textureLevel{delayed, 0, 0}}, {target: textureLevel{delayed, 2, 0}, source: textureLevel{delayed, 1, 0}}, {target: textureLevel{delayed, 3, 0}, source: textureLevel{delayed, 2, 0}}},
		nil,
	} {
		buffers := readSubmittedBuffers(frames[frameIndex])
		wantBufferCount := 1
		if want != nil {
			wantBufferCount = 2
		}
		if len(buffers) != wantBufferCount {
			t.Errorf("frame %d submitted %d command lists, want %d: the loader's, after render's own when it generates mips", frameIndex, len(buffers), wantBufferCount)
			continue
		}
		if got := readMipPasses(buffers[0], viewLevels, groupLevels); !slices.Equal(got, want) {
			t.Errorf("frame %d's first list, render's own, drew %v, want %v", frameIndex, got, want)
		}
		if got := readMipPasses(buffers[len(buffers)-1], viewLevels, groupLevels); len(got) != 0 {
			t.Errorf("frame %d's last list, the loader's, drew %v, want nothing", frameIndex, got)
		}
	}
	if created, ok := readTextureCalls(calls).findCreated[recording.CreateTextureArguments](plain); !ok || created.Descriptor.MipLevelCount != 1 {
		t.Errorf("the texture acquired without Mipmaps was made %+v, want it made once with one mip level", created.Descriptor)
	}
	checkMipBindGroupsReleasedAfterTheirSubmit(t, calls, groupLevels)
}

func TestTheMipShaderIsReadFromRendersBuiltInAssetsUnderCogRenderAndAMountHoldingItsNameOverridesIt(t *testing.T) {
	builtIn, err := shader.Preprocess(os.DirFS("../assets"), "mipshader.wgsl", nil)
	if err != nil {
		t.Fatal(err)
	}
	mask, _ := makePNG(t, 4, 4, 255)
	for _, test := range []struct {
		name  string
		files fstest.MapFS
		want  string
	}{
		{name: "built in", files: fstest.MapFS{"mask.png": {Data: mask}}, want: builtIn},
		{name: "overridden", files: fstest.MapFS{"mask.png": {Data: mask}, "cog.render/mipshader.wgsl": {Data: []byte("fn the_games_own() {}\n")}}, want: "fn the_games_own() {}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			h.Spawn(nil, &loaderPlugin{files: test.files, duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
				if frameIndex == 0 {
					inventory.AcquireTexture("mask.png", render.TextureOptions{Mipmaps: true})
				}
			}})

			h.Step()

			var preprocessed []string
			var compiled []string
			for _, call := range h.Calls() {
				switch arguments := call.Arguments.(type) {
				case recording.PreprocessShaderArguments:
					preprocessed = append(preprocessed, arguments.Path)
				case recording.CreateShaderModuleArguments:
					compiled = append(compiled, arguments.Descriptor.WGSL)
				}
			}
			if want := []string{"cog.render/mipshader.wgsl"}; !slices.Equal(preprocessed, want) {
				t.Errorf("render preprocessed %v, want %v", preprocessed, want)
			}
			if want := []string{test.want}; !slices.Equal(compiled, want) {
				t.Errorf("render compiled %q, want %q", compiled, want)
			}
		})
	}
}

// textureLevel is one mip level and layer of one of render's textures.
type textureLevel struct {
	texture         render.Texture
	mipLevel, layer uint32
}

// mipPass is one pass of render's own list: the level it draws into, and the
// level its bind group samples.
type mipPass struct {
	target, source textureLevel
}

// readTextureLevels answers the level and layer each view calls made of one
// of render's textures starts at, by view, and those of the view each bind
// group calls made binds, by group.
func readTextureLevels(calls []recording.Call) (map[gfx.TextureView]textureLevel, map[gfx.BindGroup]textureLevel) {
	textures := map[gfx.Texture]render.Texture{}
	viewLevels := map[gfx.TextureView]textureLevel{}
	groupLevels := map[gfx.BindGroup]textureLevel{}
	for _, call := range calls {
		switch arguments := call.Arguments.(type) {
		case recording.CreateTextureArguments:
			if texture, ok := handle.ParseLabel[render.Texture](arguments.Descriptor.Label); ok {
				textures[arguments.Texture] = texture
			}
		case recording.CreateTextureViewArguments:
			if texture, ok := textures[arguments.Texture]; ok {
				viewLevels[arguments.View] = textureLevel{texture, arguments.Descriptor.BaseMipLevel, arguments.Descriptor.BaseArrayLayer}
			}
		case recording.CreateBindGroupArguments:
			for _, entry := range arguments.Descriptor.Entries {
				if level, ok := viewLevels[entry.TextureView]; ok {
					groupLevels[arguments.Group] = level
				}
			}
		}
	}
	return viewLevels, groupLevels
}

// checkMipBindGroupsReleasedAfterTheirSubmit fails t unless each bind group
// of groupLevels, which calls made, is released once, after the submit of
// the frame it was made in and before the next.
func checkMipBindGroupsReleasedAfterTheirSubmit(t *testing.T, calls []recording.Call, groupLevels map[gfx.BindGroup]textureLevel) {
	t.Helper()
	// submitCounts is how many submits came before each group's make, and
	// releases how many came before each of its releases.
	submitCounts := map[gfx.BindGroup]int{}
	releases := map[gfx.BindGroup][]int{}
	submitCount := 0
	for _, call := range calls {
		switch call.Kind {
		case recording.CallKindSubmit:
			submitCount++
		case recording.CallKindCreateBindGroup:
			submitCounts[call.Arguments.(recording.CreateBindGroupArguments).Group] = submitCount
		case recording.CallKindReleaseBindGroup:
			group := call.Arguments.(gfx.BindGroup)
			releases[group] = append(releases[group], submitCount)
		}
	}
	for group := range groupLevels {
		if got := releases[group]; len(got) != 1 || got[0] != submitCounts[group]+1 {
			t.Errorf("a bind group the mips sampled %v through was released after %v submits, want once, after %d", groupLevels[group], got, submitCounts[group]+1)
		}
	}
}

// readSubmittedBuffers answers every command buffer frame submitted, in the
// order submitted.
func readSubmittedBuffers(frame recording.Frame) []recording.CommandBuffer {
	var buffers []recording.CommandBuffer
	for _, submission := range frame.Submissions {
		buffers = append(buffers, submission.Buffers...)
	}
	return buffers
}

// readMipPasses answers the passes buffer holds, each with the level it draws
// into and the level its bind group samples, by viewLevels and groupLevels.
func readMipPasses(buffer recording.CommandBuffer, viewLevels map[gfx.TextureView]textureLevel, groupLevels map[gfx.BindGroup]textureLevel) []mipPass {
	var passes []mipPass
	for _, command := range buffer.Commands {
		if command.Kind != recording.CommandKindBeginRenderPass {
			continue
		}
		pass := mipPass{target: viewLevels[command.Arguments.(gfx.RenderPassDescriptor).ColorAttachments[0].View]}
		for _, passCommand := range command.Commands {
			if passCommand.Kind == recording.CommandKindSetBindGroup {
				pass.source = groupLevels[passCommand.Arguments.(recording.SetBindGroupArguments).Group]
			}
		}
		passes = append(passes, pass)
	}
	return passes
}
