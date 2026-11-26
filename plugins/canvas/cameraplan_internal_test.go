package canvas

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

func TestACamerasPlanIsTheRowOfItsSamplesClearAndViewportAndEncodesAsItsTargetsFormatAsks(t *testing.T) {
	// A window of 64 by 48 pixels, and a texture of 64 by 32. The part
	// viewport is the window's right half.
	windowTarget := drawnTarget{width: 64, height: 48, pixelsPerPoint: 1, format: render.TextureFormatBGRA8Unorm}
	halfViewport := m.Rect{X: 0.5, Width: 0.5, Height: 1}
	clearColor := m.NewColorSrgb(0.5, 0.25, 1, 0.5)
	loadedClearColor := makeLoadedClearColor(clearColor, true)
	makeMultisampledDescriptor := func(width, height uint32) render.TextureDescriptor {
		return render.TextureDescriptor{Label: "canvas multisampled camera", Width: width, Height: height, SampleCount: 4, Format: render.TextureFormatBGRA8Unorm}
	}
	// planBlock is what a row checks of the plan's camera block.
	type planBlock struct {
		viewportOrigin, viewportSize [2]float32
		pixelSize                    float32
		encode                       uint32
	}
	for _, row := range []struct {
		name   string
		camera Camera
		target drawnTarget
		// want is the plan but for its target, view and block, and
		// wantBlock what it checks of the block.
		want      cameraPlan
		wantBlock planBlock
	}{
		{
			name:      "of one sample, clearing the whole target",
			camera:    Camera{Clear: true, ClearColor: clearColor},
			target:    windowTarget,
			want:      cameraPlan{entriesTarget: render.ColorTarget{Clear: true, ClearColor: loadedClearColor}, clear: clearByLoad},
			wantBlock: planBlock{viewportSize: [2]float32{64, 48}, pixelSize: 1, encode: 1},
		},
		{
			name:      "of one sample, clearing part of the target",
			camera:    Camera{Viewport: halfViewport, Clear: true, ClearColor: clearColor},
			target:    windowTarget,
			want:      cameraPlan{clear: clearByQuad, viewportQuad: quadClear},
			wantBlock: planBlock{viewportOrigin: [2]float32{32, 0}, viewportSize: [2]float32{32, 48}, pixelSize: 1, encode: 1},
		},
		{
			name:      "of one sample, not clearing",
			camera:    Camera{Viewport: halfViewport, ClearColor: clearColor},
			target:    windowTarget,
			wantBlock: planBlock{viewportOrigin: [2]float32{32, 0}, viewportSize: [2]float32{32, 48}, pixelSize: 1, encode: 1},
		},
		{
			name:   "of four samples, clearing the whole target",
			camera: Camera{Clear: true, ClearColor: clearColor, SampleCount: 4},
			target: windowTarget,
			want: cameraPlan{
				entriesTarget:          render.ColorTarget{Clear: true, ClearColor: loadedClearColor, Discard: true},
				multisampledDescriptor: makeMultisampledDescriptor(64, 48),
				clear:                  clearByLoad, viewportQuad: quadResolved,
				composite: compositeReplace, compositeTarget: render.ColorTarget{Clear: true, ClearColor: loadedClearColor},
			},
			wantBlock: planBlock{viewportSize: [2]float32{64, 48}, pixelSize: 1, encode: 1},
		},
		{
			name:   "of four samples, clearing part of the target",
			camera: Camera{Viewport: halfViewport, Clear: true, ClearColor: clearColor, SampleCount: 4},
			target: windowTarget,
			want: cameraPlan{
				entriesTarget:          render.ColorTarget{Clear: true, ClearColor: loadedClearColor, Discard: true},
				multisampledDescriptor: makeMultisampledDescriptor(32, 48), colorTargetOrigin: m.Vec2i{X: 32},
				clear: clearByLoad, viewportQuad: quadResolved, composite: compositeReplace,
			},
			wantBlock: planBlock{viewportSize: [2]float32{32, 48}, pixelSize: 1, encode: 1},
		},
		{
			name:   "of four samples, not clearing",
			camera: Camera{Viewport: halfViewport, ClearColor: clearColor, SampleCount: 4},
			target: windowTarget,
			want: cameraPlan{
				entriesTarget:          render.ColorTarget{Clear: true, Discard: true},
				multisampledDescriptor: makeMultisampledDescriptor(32, 48), colorTargetOrigin: m.Vec2i{X: 32},
				viewportQuad: quadResolved, composite: compositeBlend,
			},
			wantBlock: planBlock{viewportSize: [2]float32{32, 48}, pixelSize: 1, encode: 1},
		},
		{
			name:      "into a texture of a format encoding nothing",
			camera:    Camera{Viewport: m.Rect{X: 0.25, Y: 0.5, Width: 0.5, Height: 0.5}},
			target:    drawnTarget{width: 64, height: 32, pixelsPerPoint: 1, format: render.TextureFormatRGBA8Unorm},
			wantBlock: planBlock{viewportOrigin: [2]float32{16, 16}, viewportSize: [2]float32{32, 16}, pixelSize: 1, encode: 1},
		},
		{
			name:      "into a texture of an sRGB format",
			camera:    Camera{Viewport: m.Rect{X: 0.25, Y: 0.5, Width: 0.5, Height: 0.5}},
			target:    drawnTarget{width: 64, height: 32, pixelsPerPoint: 1, format: render.TextureFormatRGBA8UnormSrgb},
			wantBlock: planBlock{viewportOrigin: [2]float32{16, 16}, viewportSize: [2]float32{32, 16}, pixelSize: 1},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			plan, ok := makeCameraPlan(&row.camera, row.target)
			if !ok {
				t.Fatalf("the camera has no plan, want one")
			}
			block := plan.block
			gotBlock := planBlock{viewportOrigin: block.viewportOrigin, viewportSize: block.viewportSize, pixelSize: block.pixelSize, encode: block.encode}
			if gotBlock != row.wantBlock {
				t.Errorf("the plan's camera block is %+v, want %+v", gotBlock, row.wantBlock)
			}
			plan.view = cameraView{}
			plan.block = cameraBlock{}
			want := row.want
			want.target = row.target
			if plan != want {
				t.Errorf("the camera's plan is\n%+v\nwant\n%+v", plan, want)
			}
		})
	}
}

func TestACameraOfAnEmptyViewportHasNoPlan(t *testing.T) {
	target := drawnTarget{width: 64, height: 48, pixelsPerPoint: 1, format: render.TextureFormatBGRA8Unorm}
	if plan, ok := makeCameraPlan(&Camera{Viewport: m.Rect{Width: 0.5}, Clear: true}, target); ok {
		t.Errorf("a camera of an empty viewport has the plan %+v, want none", plan)
	}
}
