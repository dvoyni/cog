package rendertest_test

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// findPassStateCommands answers the commands among commands that set the
// viewport, the scissor rectangle, the stencil reference or the blend
// constant, and the draws, in order.
func findPassStateCommands(commands []recording.Command) []recording.Command {
	var found []recording.Command
	for _, command := range commands {
		switch command.Kind {
		case recording.CommandKindSetViewport, recording.CommandKindSetScissorRect, recording.CommandKindSetStencilReference,
			recording.CommandKindSetBlendConstant, recording.CommandKindDraw, recording.CommandKindDrawIndexed:
			found = append(found, command)
		}
	}
	return found
}

func TestPassStateSetBetweenDrawsReachesTheGPUAndANewPassStartsWithTheWholeTarget(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene drawScene
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})
		list := frame.BeginCommandList("draws", drawOrder)
		pass := list.BeginPass(scene.colorPass("first"))
		pass.SetMaterial(material)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.SetViewport(render.Viewport{X: -4, Y: 1, Width: 16, Height: 4, MaxDepth: 1})
		pass.SetScissorRectangle(render.ScissorRectangle{X: 1, Y: 2, Width: 3, Height: 4})
		pass.SetStencilReference(7)
		pass.SetBlendConstant(render.Color{R: 0.5, A: 1})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		pass = list.BeginPass(scene.colorPass("second"))
		pass.SetMaterial(material)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		list.End()
	})

	draw := recording.Command{Kind: recording.CommandKindDrawIndexed, Arguments: gfx.DrawIndexedArgs{IndexCount: 6, InstanceCount: 1}}
	want := []recording.Command{
		draw,
		{Kind: recording.CommandKindSetViewport, Arguments: gfx.Viewport{X: -4, Y: 1, Width: 16, Height: 4, MaxDepth: 1}},
		{Kind: recording.CommandKindSetScissorRect, Arguments: gfx.ScissorRect{X: 1, Y: 2, Width: 3, Height: 4}},
		{Kind: recording.CommandKindSetStencilReference, Arguments: uint32(7)},
		{Kind: recording.CommandKindSetBlendConstant, Arguments: gfx.Color{R: 0.5, A: 1}},
		draw,
		draw,
	}
	if commands := findPassStateCommands(findPassCommands(h, "first")); !reflect.DeepEqual(commands, want) {
		t.Errorf("the first pass recorded\n%+v\nwant\n%+v", commands, want)
	}
	if commands := findPassStateCommands(findPassCommands(h, "second")); !reflect.DeepEqual(commands, []recording.Command{draw}) {
		t.Errorf("the second pass recorded %+v, want its draw alone, with the whole target", commands)
	}
}

func TestAScissorRectanglePastTheTargetIsClampedToIt(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene drawScene
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetScissorRectangle(render.ScissorRectangle{X: 6, Y: 2, Width: 10, Height: 3})
			pass.SetScissorRectangle(render.ScissorRectangle{X: 20, Y: 4, Width: 4, Height: 1 << 31})
		})
	})

	want := []recording.Command{
		{Kind: recording.CommandKindSetScissorRect, Arguments: gfx.ScissorRect{X: 6, Y: 2, Width: 2, Height: 3}},
		{Kind: recording.CommandKindSetScissorRect, Arguments: gfx.ScissorRect{X: 8, Y: 4, Height: 4}},
	}
	if commands := findPassStateCommands(findPassCommands(h, "scene")); !reflect.DeepEqual(commands, want) {
		t.Errorf("the pass recorded\n%+v\nwant\n%+v: each rectangle clamped to the 8x8 target", commands, want)
	}
}

func TestAViewportWebGPURefusesIsRefusedAndReportedKeepingTheOneBefore(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindPassStateRefused)
	var scene drawScene
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetViewport(render.Viewport{Width: 8, Height: 8, MinDepth: 0.5, MaxDepth: 0.25})
			pass.SetViewport(render.Viewport{Width: 8, Height: 8, MaxDepth: 2})
			pass.SetViewport(render.Viewport{Width: -1, Height: 8, MaxDepth: 1})
			pass.SetViewport(render.Viewport{X: 1 << 20, Width: 8, Height: 8, MaxDepth: 1})
		})
	})

	if count := countReports(h, render.ReportKindPassStateRefused); count != 4 {
		t.Errorf("render reported %d refused viewports, want four: a reversed depth range, one past 1, a negative width and one past the viewport's range", count)
	}
	if commands := findPassStateCommands(findPassCommands(h, "scene")); len(commands) != 0 {
		t.Errorf("the pass recorded %+v, want no viewport", commands)
	}
}
