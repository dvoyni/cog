package canvas_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// handDrawnInstance mirrors canvas.wgsl's CanvasInstanceData without an
// argument, 144 bytes, as canvas's own instance data does.
type handDrawnInstance struct {
	tint                    m.Color
	region                  m.Rect
	crop                    m.Box2
	sliceFrame, sliceSquare m.Box2
	transform               m.Mat2x3
	page, part              uint32
	size                    m.Vec2
	width, radius           float32
	decode, mode            uint32
	tiles                   m.Vec2
}

// handDrawnShaderPath is the path handDrawnUser mounts canvas's default
// shader at, its include beside it.
const handDrawnShaderPath = "hand-drawn/default.wgsl"

// makeHandDrawnFiles answers canvas's default shader, its include and
// their binding declarations, under hand-drawn/, for handDrawnUser to
// mount with no canvas spawned.
func makeHandDrawnFiles(t testing.TB) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, name := range []string{"default.wgsl", "default.wgsl.json", "canvas.wgsl"} {
		addModuleFile(t, files, "assets/"+name)
		files["hand-drawn/"+name] = files["assets/"+name]
		delete(files, "assets/"+name)
	}
	return files
}

// handDrawnUser is the benchmarks' test plugin drawing by hand on render,
// with no canvas spawned, the frame a gateRecording of a spritesOnly
// scenario draws on canvas: each
// frame it writes every sprite's instance data into one temporary buffer,
// and draws it through each camera as a pass of canvas's default shader,
// six vertices an instance, switching between two materials of it every
// switchPeriod sprites, zero meaning never, the cameras of each order in a
// command list of their own. It skips what canvas does besides: merging
// the draw lists, sorting, culling, batching and placing images, every
// sprite a white texel's. It mounts files, makeHandDrawnFiles', itself.
// Its task's body closes over the plugin, which keeps the frame's
// instances from one frame to the next.
type handDrawnUser struct {
	files        fs.FS
	sprites      []canvas.Sprite
	switchPeriod int
	// window is the window it draws into, which the benchmark opens after
	// the spawn.
	window       platform.Window
	passes       []handDrawnPass
	colorTargets [1]render.ColorTarget
	instances    []handDrawnInstance
	// runLength is how many sprites draw with one material in a row.
	runLength int
	// materials and texture are made in its first frame.
	materials [2]render.Material
	texture   render.Texture
}

func (*handDrawnUser) Name() kernel.PluginName { return "hand-drawn user" }

func (*handDrawnUser) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (u *handDrawnUser) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket(platform.ReadMountSocket{FS: u.files})
	u.instances = make([]handDrawnInstance, len(u.sprites))
	u.runLength = len(u.sprites)
	if u.switchPeriod != 0 {
		u.runLength = u.switchPeriod
	}
	draw := configurator.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, frame render.Frame, inventory render.Inventory) kernel.NoResult {
		u.draw(frame, inventory)
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(draw, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}

func (*handDrawnUser) Start(*kernel.Kernel) error { return nil }

func (*handDrawnUser) Stop(*kernel.Kernel) error { return nil }

func (*handDrawnUser) Destroy() error { return nil }

// draw draws the frame: in its first, it makes the materials and the white
// texel's texture first.
func (u *handDrawnUser) draw(frame render.Frame, inventory render.Inventory) {
	if u.texture == (render.Texture{}) {
		shader := inventory.AcquireShader(handDrawnShaderPath)
		var state render.State
		state.ColorTargets[0].Blend = canvas.BlendNormal()
		for index := range u.materials {
			u.materials[index] = inventory.MakeMaterial(render.MaterialDescriptor{Label: "hand-drawn", Shader: shader, State: state, VertexEntryPoint: "canvas_sprite_vertex"})
		}
		u.texture = inventory.MakeTexture(render.TextureDescriptor{Label: "hand-drawn white", Width: 1, Height: 1, Format: render.TextureFormatRGBA8Unorm}, []byte{255, 255, 255, 255})
	}
	for index := range u.sprites {
		sprite := &u.sprites[index]
		size := sprite.Size
		u.instances[index] = handDrawnInstance{
			tint:      m.White,
			region:    m.Rect{Width: 1, Height: 1},
			crop:      m.Box2{Max: m.Vec2{X: 1, Y: 1}},
			transform: sprite.Transform.Mul(m.Mat2x3{size.X, 0, 0, size.Y, -size.X / 2, -size.Y / 2}),
			size:      size,
		}
	}
	list := frame.BeginCommandList("hand-drawn", u.passes[0].order)
	instances := list.AcquireTemporaryBuffer(u.instances)
	for index := range u.passes {
		passDrawn := &u.passes[index]
		if index > 0 && passDrawn.order != u.passes[index-1].order {
			list.End()
			list = frame.BeginCommandList("hand-drawn", passDrawn.order)
			instances = list.AcquireTemporaryBuffer(u.instances)
		}
		u.colorTargets[0] = render.ColorTarget{Window: u.window, Clear: passDrawn.clear, ClearColor: render.Color{A: 1}}
		pass := list.BeginPass(render.PassDescriptor{Label: "hand-drawn camera", ColorTargets: u.colorTargets[:]})
		pass.SetViewport(passDrawn.viewport)
		pass.SetPassUniform("canvas_camera", passDrawn.camera)
		for start := 0; start < len(u.sprites); start += u.runLength {
			pass.SetMaterial(u.materials[start/u.runLength%2])
			pass.SetBuffer("canvas_instances", instances)
			pass.SetTextureView("canvas_atlas", render.TextureView{Texture: u.texture})
			pass.SetTextureView("canvas_sprite_atlas", render.TextureView{Texture: u.texture})
			pass.SetSampler("canvas_sampler", render.SamplerDescriptor{})
			pass.DrawVertices(render.PrimitiveTopologyTriangleList, 6, render.DrawArgs{InstanceCount: uint32(min(u.runLength, len(u.sprites)-start)), FirstInstance: uint32(start)})
		}
		pass.End()
	}
	list.End()
}
