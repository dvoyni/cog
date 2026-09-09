package canvas

// THROWAWAY. Prototype measurement for https://github.com/dvoyni/cog/issues/153,
// on branch proto/sprite-collapse. It asserts nothing: it reflects both sprite
// shaders through the real front end and prints what a draw down each route
// actually costs, so the ticket's answer carries numbers rather than argument.
//
//	go test ./canvas -run TestProto153 -v
//
// The in-package testBackend cannot supply these numbers: its ShaderLayout is
// one hand-written union for every shader (canvas_test.go:115), with `instances`
// at group 2 where spritebatch.wgsl really declares it at group 1. So this reads
// the shaders themselves.

import (
	"fmt"
	"sort"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
	"testing/fstest"
)

// shaderCost is what one draw down a shader's route binds.
type shaderCost struct {
	path        string
	uniformSize uint32
	uniforms    []string
	groups      map[uint32][]string
	storage     []string
}

func measure(t *testing.T, path string) shaderCost {
	t.Helper()
	parsed, err := naga.Parse(flattenBuiltinShader(t, path))
	if err != nil {
		t.Fatalf("parse %q: %v", path, err)
	}
	module, err := wgsl.Lower(parsed)
	if err != nil {
		t.Fatalf("lower %q: %v", path, err)
	}
	cost := shaderCost{path: path, groups: map[uint32][]string{}}
	for _, variable := range module.GlobalVariables {
		if variable.Binding == nil {
			continue
		}
		group, binding := variable.Binding.Group, variable.Binding.Binding
		kind := "?"
		switch variable.Space {
		case ir.SpaceUniform:
			kind = "uniform"
		case ir.SpaceHandle:
			kind = "handle"
		default:
			kind = "storage"
		}
		cost.groups[group] = append(cost.groups[group],
			fmt.Sprintf("@binding(%d) %s %s", binding, kind, variable.Name))

		inner := module.Types[variable.Type].Inner
		if variable.Space == ir.SpaceUniform {
			structure, ok := inner.(ir.StructType)
			if !ok {
				continue
			}
			cost.uniformSize = structure.Span
			for _, member := range structure.Members {
				cost.uniforms = append(cost.uniforms,
					fmt.Sprintf("%s@%d", member.Name, member.Offset))
			}
		}
		if variable.Space != ir.SpaceUniform && variable.Space != ir.SpaceHandle {
			cost.storage = append(cost.storage, variable.Name)
		}
	}
	return cost
}

func (c shaderCost) report(t *testing.T) {
	t.Helper()
	keys := make([]int, 0, len(c.groups))
	for group := range c.groups {
		keys = append(keys, int(group))
	}
	sort.Ints(keys)
	t.Logf("--- %s", c.path)
	t.Logf("    bind groups: %d", len(keys))
	for _, group := range keys {
		sort.Strings(c.groups[uint32(group)])
		t.Logf("      @group(%d): %v", group, c.groups[uint32(group)])
	}
	t.Logf("    uniform block: %d bytes, padded to %d by gfx.StorageAlignment",
		c.uniformSize, roundUp(int(c.uniformSize), gfx.StorageAlignment))
	t.Logf("    uniform members: %v", c.uniforms)
	t.Logf("    storage buffers: %v", c.storage)
}

func roundUp(value, alignment int) int {
	if alignment <= 0 || value%alignment == 0 {
		return value
	}
	return value + alignment - value%alignment
}

// TestProto153ShaderCost prints what each route binds per draw.
func TestProto153ShaderCost(t *testing.T) {
	// The uniform shader's numbers, measured on this same branch before it was
	// deleted, for the comparison the ticket asks for:
	//
	//	builtin/canvas/sprite.wgsl
	//	  bind groups: 2
	//	    @group(0): @binding(0) uniform u
	//	    @group(1): @binding(0) sampler, @binding(1) texture_2d_array
	//	  uniform block: 176 bytes, padded to 256 by gfx.StorageAlignment
	//	  storage buffers: none
	measure(t, spriteBatchShaderPath).report(t)
	t.Logf("--- SpriteInstance record: %d bytes (Go), one per instance in the storage buffer",
		int(unsafeSizeOfSpriteInstance()))
}

func unsafeSizeOfSpriteInstance() uintptr {
	var s SpriteInstance
	return sizeOf(s)
}

func sizeOf(s SpriteInstance) uintptr {
	return uintptr(len(spriteInstanceBytes([]SpriteInstance{s})))
}

// TestProto153DrawCounts records the same sprites down both routes through the
// real canvas pipeline and reports how many draws each produced.
func TestProto153DrawCounts(t *testing.T) {
	const sprites = 8
	// perSprite is a distinct material for every sprite: same shader, different
	// parameter value, so the fingerprints differ and nothing merges. This is
	// the worst case the ticket asks to price - every draw with its own
	// material and every batch one instance.
	perSprite := make([]gfx.MaterialDescr, sprites)
	for i := range perSprite {
		perSprite[i] = gfx.MaterialWithState(
			gfx.ShaderWithResource(spriteBatchShaderPath), gfx.StateOverlay2D,
			gfx.FloatParam("fade", float32(i)/float32(sprites)),
		)
	}
	shared := gfx.MaterialWithState(
		gfx.ShaderWithResource(spriteBatchShaderPath), gfx.StateOverlay2D,
		gfx.FloatParam("fade", 0.5),
	)
	cases := []struct {
		name string
		// material is what every sprite carries; when nil, per says whether to
		// hand each sprite its own.
		material *gfx.MaterialDescr
		per      bool
	}{
		{name: "nil material"},
		{name: "DefaultMaterial()", material: DefaultMaterial()},
		{name: "one shared custom material", material: &shared},
		{name: "a distinct material per sprite", per: true},
	}
	for _, testCase := range cases {
		filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}}
		config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
		k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
			for i := range sprites {
				material := testCase.material
				if testCase.per {
					material = &perSprite[i]
				}
				write.Sprite(0, "sprite.png", SpriteTransform{
					Position: m.Vec2{X: float32(i) * 8, Y: 10},
					Size:     m.Vec2{X: 6, Y: 6},
				}, material)
			}
		})
		runFrame(k)
		// The instance buffers are the ones whose size is a whole number of
		// SpriteInstance records; the atlas upload and the quad are not.
		record := len(spriteInstanceBytes([]SpriteInstance{{}}))
		instanceBuffers, instanceBytes := 0, 0
		for _, buffer := range backend.buffers {
			if len(buffer.data) > 0 && len(buffer.data)%record == 0 && len(buffer.data) <= sprites*record {
				instanceBuffers++
				instanceBytes += len(buffer.data)
			}
		}
		t.Logf("%-32s %d sprites -> %d draws | %d uniform payloads of %d B | %d instance buffers, %d B total",
			testCase.name, sprites, backend.draws, len(backend.drawParams),
			uniformBytes(backend.drawParams), instanceBuffers, instanceBytes)
	}
}

// uniformBytes is the size of one uniform payload, or 0 when none was written.
func uniformBytes(payloads [][]byte) int {
	if len(payloads) == 0 {
		return 0
	}
	return len(payloads[0])
}
