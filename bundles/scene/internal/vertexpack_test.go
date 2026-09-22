package internal

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// everyAttribute is a mesh whose every field is written with a value nothing
// else in it shares, so a pack that swapped two attributes, wrote one at the
// wrong offset or dropped one entirely cannot come out byte-identical by
// accident.
func everyAttribute() []scene.Vertex {
	return []scene.Vertex{
		{
			Position: m.Vec3{X: 1.5, Y: -2.25, Z: 3.125},
			Normal:   m.Vec3{X: 0.5, Y: -0.5, Z: 0.7071},
			Tangent:  m.Vec4{X: -1, Y: 0.25, Z: 0.75, W: -1},
			UV0:      m.Vec2{X: 0.125, Y: 0.875},
			UV1:      m.Vec2{X: 18.52, Y: -13.49},
			Color:    m.NewColorLinear(1.0/255, 2.0/255, 3.0/255, 4.0/255),
		},
		{
			Position: m.Vec3{X: -7, Y: 11, Z: 0.03125},
			Normal:   m.Vec3{X: -1},
			Tangent:  m.Vec4{X: 0, Y: 1, Z: 0, W: 1},
			UV0:      m.Vec2{X: 1, Y: 0},
			UV1:      m.Vec2{X: -0.5, Y: 2.5},
			Color:    m.NewColorLinear(1, 254.0/255, 253.0/255, 252.0/255),
		},
	}
}

func readFloat32(at []byte) float32 { return math.Float32frombits(binary.NativeEndian.Uint32(at)) }

// The bake path stages packed bytes, not a copy of the caller's slice. The
// drain is the only place that is observable: downstream of it the vertices are
// a buffer id and a size, so the probe drains the Lookup itself and keeps the
// first bytes it is handed, which are the vertices.
func TestBakeMeshStagesThePackedVertices(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) { q.Camera(testCamera, testCameraDescr()) })
	vertices := everyAttribute()
	ref := h.bake(vertices, nil, gfx.TopologyLineList)
	if ref.ID() == 0 {
		t.Fatal("the bake was refused")
	}

	var staged []byte
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *scene.Lookup) {
		lookup.DrainMeshes(model.MeshBaker{
			Bake: func(data []byte) gfx.BufferDescr {
				if staged == nil {
					staged = append([]byte{}, data...)
				}
				return gfx.BufferDescr{}
			},
			Release: func(gfx.BufferDescr) {},
		})
	}})
	var arena []byte
	at, _, _ := model.PackVertices(&arena, vertices)
	want := at.Of(arena)
	if len(staged) != len(want) {
		t.Fatalf("staged %d vertex bytes, want %d", len(staged), len(want))
	}
	for i := range want {
		if staged[i] != want[i] {
			t.Fatalf("staged byte %d is %#02x, want %#02x", i, staged[i], want[i])
		}
	}
}
