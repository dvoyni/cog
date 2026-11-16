package render_test

import (
	"maps"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestTwoAcquiresOfOneDescriptorInAFrameGetTwoTexturesAndTheNextFrameReusesThem(t *testing.T) {
	h := rendertest.NewHarness(t)
	descriptor := render.TextureDescriptor{Label: "bloom", Width: 32, Height: 32, Format: render.TextureFormatRGBA16Float}
	var acquiredTextures [][]render.Texture
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		acquiredTextures = append(acquiredTextures, []render.Texture{frame.AcquireTemporaryTexture(descriptor), frame.AcquireTemporaryTexture(descriptor)})
	}}})

	h.Step()
	h.Step()

	if len(acquiredTextures) != 2 {
		t.Fatalf("the recording task ran %d times, want 2", len(acquiredTextures))
	}
	if first := acquiredTextures[0]; first[0] == first[1] || slices.Contains(first, render.Texture{}) {
		t.Errorf("the first frame's two acquires got %v, want two textures", first)
	}
	if !slices.Equal(acquiredTextures[1], acquiredTextures[0]) {
		t.Errorf("the second frame's acquires got %v, want the first frame's %v", acquiredTextures[1], acquiredTextures[0])
	}
}

func TestTasksAcquiringAndDrawingIntoTemporariesConcurrentlyGetTexturesOfTheirOwn(t *testing.T) {
	h := rendertest.NewHarness(t)
	descriptor := render.TextureDescriptor{Label: "bloom", Width: 32, Height: 32, Format: render.TextureFormatRGBA8Unorm}
	// mutex guards acquiredTextures, what both tasks acquired.
	var mutex sync.Mutex
	var acquiredTextures []render.Texture
	makeRecord := func(order render.Order) func(render.Frame) {
		return func(frame render.Frame) {
			list := frame.BeginCommandList("bloom", order)
			for range 16 {
				texture := frame.AcquireTemporaryTexture(descriptor)
				recordPass(&list, render.PassDescriptor{Label: "bloom", ColorTargets: []render.ColorTarget{{Texture: texture}}})
				mutex.Lock()
				acquiredTextures = append(acquiredTextures, texture)
				mutex.Unlock()
			}
			list.End()
		}
	}
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){makeRecord(1), makeRecord(2)}})

	got := stepAndReadFrame(t, h)

	mutex.Lock()
	defer mutex.Unlock()
	distinctTextures := map[render.Texture]bool{}
	for _, texture := range acquiredTextures {
		distinctTextures[texture] = true
	}
	if len(distinctTextures) != 32 || distinctTextures[render.Texture{}] {
		t.Errorf("two tasks acquiring 16 temporaries each got %d distinct textures among %v, want 32", len(distinctTextures), acquiredTextures)
	}
	var drawnTextures []render.Texture
	drawnDistinctTextures := map[render.Texture]bool{}
	for _, list := range got.CommandLists {
		for _, pass := range list.Passes {
			drawnTextures = append(drawnTextures, pass.ColorTargets[0].Texture)
			drawnDistinctTextures[pass.ColorTargets[0].Texture] = true
		}
	}
	if len(drawnTextures) != len(acquiredTextures) || !maps.Equal(drawnDistinctTextures, distinctTextures) {
		t.Errorf("the frame reads back passes into %v, want one into each texture acquired, %v", drawnTextures, acquiredTextures)
	}
}

func TestPassesIntoTemporaryTexturesAtAMipLevelAndLayerWithADepthTargetAndWithResolvesRecordAsDeclared(t *testing.T) {
	h := rendertest.NewHarness(t)
	var window platform.Window
	red := render.Color{R: 1, A: 1}
	var passes []render.InspectedPass
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		cascades := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "cascades", Width: 64, Height: 48, LayerCount: 3, MipLevelCount: 3, Format: render.TextureFormatRGBA8Unorm})
		mipSized := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "mip sized", Width: 16, Height: 12, Format: render.TextureFormatRGBA8Unorm})
		color := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "color", Width: 64, Height: 48, Format: render.TextureFormatRGBA8Unorm})
		depth := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "depth", Width: 64, Height: 48, Format: render.TextureFormatDepth24PlusStencil8})
		depthOnly := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "depth only", Width: 64, Height: 48, Format: render.TextureFormatDepth32Float})
		toWindow := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "to window", Width: 64, Height: 48, SampleCount: 4, Format: render.TextureFormatBGRA8Unorm})
		toTexture := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "to texture", Width: 64, Height: 48, SampleCount: 4, Format: render.TextureFormatRGBA8Unorm})
		resolved := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "resolved", Width: 64, Height: 48, Format: render.TextureFormatRGBA8Unorm})
		passes = []render.InspectedPass{
			{Label: "mip", ColorTargets: []render.ColorTarget{{Texture: cascades, MipLevel: 2, Layer: 1, Clear: true, ClearColor: red}, {Texture: mipSized}}},
			{Label: "layer", ColorTargets: []render.ColorTarget{{Texture: cascades, MipLevel: 2, Layer: 2}}},
			{Label: "shadow", DepthStencil: render.DepthStencilTarget{Texture: depth, Clear: true, ClearDepth: 1, ClearStencil: 7}},
			{Label: "depth test", ColorTargets: []render.ColorTarget{{Texture: color, Discard: true}}, DepthStencil: render.DepthStencilTarget{Texture: depth, ReadOnly: true}},
			{Label: "depth-only test", ColorTargets: []render.ColorTarget{{Texture: color}}, DepthStencil: render.DepthStencilTarget{Texture: depthOnly, ReadOnly: true}},
			{Label: "resolve", ColorTargets: []render.ColorTarget{
				{Texture: toWindow, Discard: true, Resolve: render.ResolveTarget{Window: window}},
				{Texture: toTexture, Resolve: render.ResolveTarget{Texture: resolved}},
			}},
		}
		list := frame.BeginCommandList("offscreen", 1)
		for _, pass := range passes {
			recordPass(&list, render.PassDescriptor{Label: pass.Label, ColorTargets: pass.ColorTargets, DepthStencil: pass.DepthStencil})
		}
		list.End()
	}}})
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})

	got := stepAndReadFrame(t, h)

	for index := range passes {
		passes[index].Position = index
	}
	want := render.Inspection{CommandLists: []render.InspectedCommandList{{Label: "offscreen", Passes: passes}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the frame reads back as %+v, want %+v", got, want)
	}
}

func TestAPassWhoseTextureTargetsAreMissingOfTheWrongKindOrDisagreeIsRefusedReportedOnceAndRecordsNothing(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindPassTargetsRefused)
	var window platform.Window
	var kept render.Texture
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		color := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "color", Width: 64, Height: 48, Format: render.TextureFormatRGBA8Unorm})
		small := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "small", Width: 32, Height: 32, Format: render.TextureFormatRGBA8Unorm})
		mipped := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "mipped", Width: 64, Height: 48, MipLevelCount: 3, Format: render.TextureFormatRGBA8Unorm})
		depth := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "depth", Width: 64, Height: 48, Format: render.TextureFormatDepth32Float})
		multisampled := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "multisampled", Width: 64, Height: 48, SampleCount: 4, Format: render.TextureFormatRGBA8Unorm})
		kept = color
		list := frame.BeginCommandList("offscreen", 1)
		for _, descriptor := range []render.PassDescriptor{
			{Label: "both", ColorTargets: []render.ColorTarget{{Window: window, Texture: color}}},
			{Label: "no such mip level", ColorTargets: []render.ColorTarget{{Texture: mipped, MipLevel: 3}}},
			{Label: "no such layer", ColorTargets: []render.ColorTarget{{Texture: color, Layer: 1}}},
			{Label: "depth as colour", ColorTargets: []render.ColorTarget{{Texture: depth}}},
			{Label: "colour as depth", DepthStencil: render.DepthStencilTarget{Texture: color}},
			{Label: "sizes", ColorTargets: []render.ColorTarget{{Texture: color}, {Texture: small}}},
			{Label: "window and texture sizes", ColorTargets: []render.ColorTarget{{Window: window}, {Texture: small}}},
			{Label: "depth size", ColorTargets: []render.ColorTarget{{Texture: small}}, DepthStencil: render.DepthStencilTarget{Texture: depth}},
			{Label: "sample counts", ColorTargets: []render.ColorTarget{{Texture: color}, {Texture: multisampled}}},
			{Label: "resolve of one sample", ColorTargets: []render.ColorTarget{{Texture: small, Resolve: render.ResolveTarget{Texture: small}}}},
			{Label: "read-only cleared", ColorTargets: []render.ColorTarget{{Texture: color}}, DepthStencil: render.DepthStencilTarget{Texture: depth, ReadOnly: true, Clear: true}},
			{Label: "resolve of another format", ColorTargets: []render.ColorTarget{{Texture: multisampled, Resolve: render.ResolveTarget{Window: window}}}},
			{Label: "kept", ColorTargets: []render.ColorTarget{{Texture: color}}},
		} {
			recordPass(&list, descriptor)
		}
		list.End()
	}}})
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})

	got := stepAndReadFrame(t, h)

	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindPassTargetsRefused}, 12); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v", kinds, want)
	}
	want := render.Inspection{CommandLists: []render.InspectedCommandList{
		{Label: "offscreen", Passes: []render.InspectedPass{{Label: "kept", ColorTargets: []render.ColorTarget{{Texture: kept}}}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the frame reads back as %+v, want only the pass kept, %+v", got, want)
	}
}

func TestATemporaryUnusedForTemporaryIdleFramesFramesIsFreedAndItsHandleRefused(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindPassTargetsRefused)
	descriptor := render.TextureDescriptor{Label: "bloom", Width: 32, Height: 32, Format: render.TextureFormatRGBA8Unorm}
	// acquiring says, beat by beat, whether the recording task acquires a
	// temporary, and drawing which of those it acquired it draws into.
	acquiring := []bool{true, false, true, false, false, true, false}
	drawing := [][]int{{0}, nil, nil, nil, nil, nil, {0, 2}}
	var beat int
	var acquiredTextures []render.Texture
	h.Spawn(&render.Config{TemporaryIdleFrames: 2}, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		if acquiring[beat] {
			acquiredTextures = append(acquiredTextures, frame.AcquireTemporaryTexture(descriptor))
		}
		list := frame.BeginCommandList("bloom", 1)
		for _, index := range drawing[beat] {
			recordPass(&list, render.PassDescriptor{Label: "bloom", ColorTargets: []render.ColorTarget{{Texture: acquiredTextures[index]}}})
		}
		list.End()
		beat++
	}}})

	for range acquiring {
		h.Step()
	}

	if len(acquiredTextures) != 3 {
		t.Fatalf("the recording task acquired %d temporaries, want 3", len(acquiredTextures))
	}
	if acquiredTextures[1] != acquiredTextures[0] {
		t.Errorf("a temporary unused for one frame was replaced by %v, want it, %v, reused", acquiredTextures[1], acquiredTextures[0])
	}
	if acquiredTextures[2] == acquiredTextures[0] {
		t.Errorf("a temporary unused for two frames was handed out again as %v, want it freed and another made", acquiredTextures[2])
	}
	if kinds, want := makeReportKinds(h), []render.ReportKind{render.ReportKindPassTargetsRefused}; !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v, for the pass into the freed temporary", kinds, want)
	}
	frames := h.Frames()
	var passes []render.InspectedPass
	for _, list := range frames[len(frames)-1].CommandLists {
		passes = append(passes, list.Passes...)
	}
	if len(passes) != 1 || passes[0].ColorTargets[0].Texture != acquiredTextures[2] {
		t.Errorf("the last frame reads back passes %v, want one into the temporary made after the freed one, %v", passes, acquiredTextures[2])
	}
}
