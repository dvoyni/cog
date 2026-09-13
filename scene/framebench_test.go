package scene

import (
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// frameBenchDraws is the population the frame benchmark records: the number
// the material design note's per-draw measurements were scaled to, so the
// frame is checked against the prediction rather than against nothing.
const frameBenchDraws = 5000

// BenchmarkFrame is one whole frame of 5 000 separately recorded Mesh calls -
// the shape an ECS System recording one draw per Entity takes - through the
// kernel, the flush, gfx's translation and a backend that draws nothing. The
// three cases differ only in what each draw says about its material:
//
//   - none: the bundled PBR, which never computes a content key;
//   - shared: one caller Material built once and named by every draw, so the
//     recording copies it and the flush keys it 5 000 times;
//   - override: that shared Material plus a one-float Params override, the
//     per-Entity variation case. Every draw overrides with the same value.
//
// The shared material is the bundled PBR's own parameter set - its five slots
// bound to scene's baked 1x1 defaults - plus one float, so it differs from
// "none" in being the caller's rather than in what it binds. Inline texture
// bytes would not do: gfx bakes an inline texture into a temporary per draw,
// and the frame would measure that instead.
//
// It reports ns, allocations and the batch count of the one pass per frame.
// Whole-frame numbers swing by about ten percent with run order, so a
// comparison is two test binaries built first and run interleaved.
func BenchmarkFrame(b *testing.B) {
	var shared Material
	var override []gfx.ParameterDescr
	cases := []struct {
		name string
		draw func(transform Transform) MeshDraw
	}{
		{"none", func(transform Transform) MeshDraw {
			return MeshDraw{Transform: transform}
		}},
		{"shared", func(transform Transform) MeshDraw {
			return MeshDraw{Transform: transform, Material: shared}
		}},
		{"override", func(transform Transform) MeshDraw {
			return MeshDraw{Transform: transform, Material: shared, Params: override}
		}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			var ref MeshRef
			recordMaterials := false
			h := newHarness(b, func(q *OpQueue) {
				q.Camera(testCamera, CameraDescr{
					Transform: LookAt(m.Vec3{Z: 80}, m.Vec3{}, m.Vec3{Y: 1}),
					FovY:      1.0472, Near: 0.1, Far: 200,
				})
				for i := range frameBenchDraws {
					x := float32(i%100)*0.5 - 25
					y := float32(i/100)*0.5 - 12.5
					draw := MeshDraw{Transform: At(x, y, 0)}
					if recordMaterials {
						draw = c.draw(At(x, y, 0))
					}
					q.Mesh(0, ref, draw)
				}
			})
			ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
			// The first frame bakes scene's defaults, which the shared
			// material binds.
			h.frame()
			if shared == nil {
				var defaults pbrDefaults
				h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(l *Lookup) {
					defaults = l.defaults
				}})
				shared, override = frameBenchMaterial(defaults)
			}
			recordMaterials = true
			for range 3 {
				h.frame()
			}
			if errs := h.errors(); len(errs) != 0 {
				b.Fatalf("the frame reported %v", errs)
			}
			pass := h.passes()[0]
			if pass.Instances != frameBenchDraws {
				b.Fatalf("packed %d instances, want all %d", pass.Instances, frameBenchDraws)
			}
			batches := len(pass.Batches)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				h.frame()
			}
			b.StopTimer()
			b.ReportMetric(float64(batches), "batches/frame")
		})
	}
}

// frameBenchMaterial builds the benchmark's shared material over scene's baked
// defaults, and the one-float override that varies it.
func frameBenchMaterial(defaults pbrDefaults) (Material, []gfx.ParameterDescr) {
	if defaults.white.ID() == 0 || defaults.flatNormal.ID() == 0 {
		panic("scene's default textures were not baked by the first frame")
	}
	params := []gfx.ParameterDescr{gfx.FloatParam("key", 1)}
	for i, slot := range pbrSlots {
		texture := defaults.white
		if i == normalSlot {
			texture = defaults.flatNormal
		}
		params = append(params,
			gfx.TextureParam(slot.texture, texture),
			gfx.SamplerParam(slot.sampler, pbrSampler),
		)
	}
	material := Material{{Descr: gfx.MaterialWithState(
		gfx.ShaderWithResource(sceneShaderPath), gfx.StateOpaque3D, params...,
	)}}
	return material, []gfx.ParameterDescr{gfx.FloatParam("key", 0.25)}
}
