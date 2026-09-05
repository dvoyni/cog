package scene

import (
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

const testCamera CameraID = -100

var testBoxColor = m.NewColorSrgb(0.42, 0.71, 0.94, 1)

// testCameraDescr is the floor of the API: a camera looking at the origin.
func testCameraDescr() CameraDescr {
	return CameraDescr{
		Transform: LookAt(m.Vec3{X: 3, Y: 2, Z: 4}, m.Vec3{}, m.Vec3{Y: 1}),
		FovY:      1.0472,
		Near:      0.1,
		Far:       100,
	}
}

func TestBoxRecordsOneOp(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Box(0, At(1, 2, 3), testBoxColor)
	})
	h.frame()

	ops := h.ops()
	if len(ops) != 2 {
		t.Fatalf("recorded %d ops, want a camera and a box", len(ops))
	}
	box := ops[1]
	if box.Kind != OpBox {
		t.Fatalf("the second op is kind %d, want OpBox", box.Kind)
	}
	if box.Transform.Position != (m.Vec3{X: 1, Y: 2, Z: 3}) {
		t.Fatalf("the box is at %v, want (1,2,3)", box.Transform.Position)
	}
	if box.Color != testBoxColor {
		t.Fatalf("the box is %v, want %v", box.Color, testBoxColor)
	}
}

func TestABoxIsOneInstancedDrawInItsCamerasPass(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
	})
	h.frame()

	passes := h.passes()
	if len(passes) != 1 {
		t.Fatalf("the frame published %d passes, want 1", len(passes))
	}
	pass := passes[0]
	if pass.Recorded != 1 || pass.Instances != 1 {
		t.Fatalf("pass recorded %d and packed %d instances, want 1 and 1", pass.Recorded, pass.Instances)
	}
	if len(pass.Batches) != 1 {
		t.Fatalf("pass has %d batches, want 1", len(pass.Batches))
	}
	batch := pass.Batches[0]
	if batch.MeshID == 0 || batch.MaterialID == 0 {
		t.Fatalf("batch is %+v, want a mesh and a material id", batch)
	}
	if batch.FirstInstance != 0 || batch.InstanceCount != 1 {
		t.Fatalf("batch spans instances [%d, %d), want [0, 1)", batch.FirstInstance, batch.FirstInstance+batch.InstanceCount)
	}
}

func TestACameraDrawsOnlyTheLayersItsMaskSelects(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, func() CameraDescr {
			descr := testCameraDescr()
			descr.CullMask = Layer(1)
			return descr
		}())
		q.Box(Layer(1), At(0, 0, 0), testBoxColor)
		q.Box(Layer(2), At(2, 0, 0), testBoxColor)
	})
	h.frame()

	passes := h.passes()
	if len(passes) != 1 {
		t.Fatalf("the frame published %d passes, want 1", len(passes))
	}
	if passes[0].Recorded != 1 || passes[0].Instances != 1 {
		t.Fatalf("pass recorded %d and packed %d, want the one box on layer 1",
			passes[0].Recorded, passes[0].Instances)
	}
}

// firstInstance is what lets a batch read its own slice with no offset
// plumbing, and the slice is per pass, so the second camera's first draw must
// start at instance 0 of its own bound range - not at 1.
func TestEveryPassBindsItsOwnInstanceSliceAndCountsFromZero(t *testing.T) {
	const second CameraID = -50
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Camera(second, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
		q.Box(0, At(2, 0, 0), testBoxColor)
	})
	h.frame()

	if len(h.backend.draws) != 4 {
		t.Fatalf("the frame made %d draws, want two boxes in each of two passes", len(h.backend.draws))
	}
	want := []int{0, 1, 0, 1}
	for i, draw := range h.backend.draws {
		if draw.firstInstance != want[i] {
			t.Fatalf("draw %d starts at instance %d, want %d", i, draw.firstInstance, want[i])
		}
		if draw.instances != 1 {
			t.Fatalf("draw %d covers %d instances, want 1", i, draw.instances)
		}
	}

	instances := h.backend.buffersBoundTo("sceneInstances")
	if len(instances) != 4 {
		t.Fatalf("sceneInstances was bound %d times, want once per draw", len(instances))
	}
	if instances[0].offset != 0 {
		t.Fatalf("the first pass binds its slice at %d, want 0", instances[0].offset)
	}
	if instances[2].offset == instances[0].offset {
		t.Fatal("both passes bound the same instance slice")
	}
	for _, binding := range instances {
		if binding.offset%gfx.StorageAlignment != 0 {
			t.Fatalf("a storage range starts at %d, which is not %d-aligned", binding.offset, gfx.StorageAlignment)
		}
		if binding.size != 2*int(unsafe.Sizeof(sceneInstance{})) {
			t.Fatalf("a pass bound %d bytes of instances, want its own two records", binding.size)
		}
	}
}

func TestEveryDrawBindsTheFrameAndItsMaterialRecord(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
		q.Box(0, At(2, 0, 0), m.NewColorSrgb(1, 0, 0, 1))
	})
	h.frame()

	frames := h.backend.buffersBoundTo("sceneFrame")
	if len(frames) != 2 {
		t.Fatalf("sceneFrame was bound %d times, want once per draw", len(frames))
	}
	if frames[0] != frames[1] {
		t.Fatalf("the two draws in one pass bound different frame blocks: %+v and %+v", frames[0], frames[1])
	}
	if frames[0].size != int(unsafe.Sizeof(sceneFrameBlock{})) {
		t.Fatalf("sceneFrame is bound as %d bytes, want the %d-byte block",
			frames[0].size, unsafe.Sizeof(sceneFrameBlock{}))
	}

	materials := h.backend.buffersBoundTo("scenePbrMaterial")
	if len(materials) != 2 {
		t.Fatalf("scenePbrMaterial was bound %d times, want once per draw", len(materials))
	}
	if materials[0].offset == materials[1].offset {
		t.Fatal("two differently coloured boxes bound one material record")
	}
	for _, binding := range materials {
		if binding.offset%gfx.StorageAlignment != 0 {
			t.Fatalf("a material record is at %d, which is not %d-aligned", binding.offset, gfx.StorageAlignment)
		}
	}
}

// One arena per binding, one upload each, whatever the frame draws.
func TestTheFrameUploadsOneBufferPerArena(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		for i := range 8 {
			q.Box(0, At(float32(i), 0, 0), testBoxColor)
		}
	})
	h.frame()
	first := h.backend.bakes
	h.backend.bakes = 0
	h.frame()

	if h.backend.bakes != 3 {
		t.Fatalf("a steady frame uploaded %d buffers, want the three arenas", h.backend.bakes)
	}
	// The first frame also bakes the unit box's two durable buffers, once.
	if first != 5 {
		t.Fatalf("the first frame uploaded %d buffers, want three arenas plus the unit box", first)
	}
}

func TestAFrameWithNoBoxesStillEmitsItsPass(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
	})
	h.frame()

	if len(h.backend.passes) == 0 {
		t.Fatal("a camera with nothing to draw emitted no pass; its clear is an effect")
	}
	if len(h.backend.draws) != 0 {
		t.Fatalf("an empty camera made %d draws", len(h.backend.draws))
	}
}

func TestTheBundledShaderIsMountedForTheFirstFrame(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {})
	embedded, err := shaderFS.ReadFile(sceneShaderPath)
	if err != nil {
		t.Fatalf("read the embedded shader: %v", err)
	}
	mounted := h.readFile(t, sceneShaderPath)
	if string(mounted) != string(embedded) {
		t.Fatal("the mounted shader differs from the embedded source")
	}
}

// WGSL requires every declared binding bound, and gfx does no preprocessing, so
// omitting a texture is not available without shader variants: scene owns the
// defaults and binds all five slots on every draw. Missing one does not degrade
// the frame - CreateBindGroup fails, its error is swallowed, and the whole
// frame's command buffer vanishes with no error anywhere.
func TestEveryDrawBindsAllFivePbrSlots(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
	})
	h.frame()

	var white []gfx.TextureID
	for _, slot := range []string{"baseColor", "metallicRoughness", "occlusion", "emissive"} {
		textures := h.backend.texturesBoundTo(slot + "Texture")
		if len(textures) != 1 || textures[0] == 0 {
			t.Fatalf("%sTexture was bound %v, want the default white texel once per draw", slot, textures)
		}
		white = append(white, textures[0])
		if samplers := h.backend.samplersBoundTo(slot + "Sampler"); len(samplers) != 1 || samplers[0] == 0 {
			t.Fatalf("%sSampler was bound %v, want one sampler per draw", slot, samplers)
		}
	}
	// One white texel serves four slots, because 1.0 is a fixed point of the
	// sRGB transfer curve: the sRGB-format and linear-format slots read the
	// same 1.0 from it.
	for _, texture := range white[1:] {
		if texture != white[0] {
			t.Fatalf("the four white-defaulted slots bound %v, want one texture for all four", white)
		}
	}
	normals := h.backend.texturesBoundTo("normalTexture")
	if len(normals) != 1 || normals[0] == 0 {
		t.Fatalf("normalTexture was bound %v, want the flat normal once per draw", normals)
	}
	if normals[0] == white[0] {
		t.Fatal("the normal slot bound the white texel; a flat normal is (0.5, 0.5, 1)")
	}
	if samplers := h.backend.samplersBoundTo("normalSampler"); len(samplers) != 1 {
		t.Fatalf("normalSampler was bound %v, want one sampler per draw", samplers)
	}
}

// The defaults are baked once, not once per frame: they are durable textures
// like any other, and rebaking them every frame would upload two texels forever.
func TestTheDefaultTexturesAreBakedOnce(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
	})
	h.frame()
	first := h.backend.texturesBoundTo("baseColorTexture")
	h.frame()
	second := h.backend.texturesBoundTo("baseColorTexture")

	if len(second) != 2 {
		t.Fatalf("two frames bound baseColorTexture %d times, want once per draw", len(second))
	}
	if second[1] != first[0] {
		t.Fatalf("the second frame bound texture %d, want the first frame's %d", second[1], first[0])
	}
}

// A material record is one per batch and 160 bytes of content, bound as a range
// padded up to the storage alignment.
func TestTheMaterialRecordIsBoundAsItsOwnRange(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
	})
	h.frame()

	materials := h.backend.buffersBoundTo("scenePbrMaterial")
	if len(materials) != 1 {
		t.Fatalf("scenePbrMaterial was bound %d times, want once per draw", len(materials))
	}
	if materials[0].size != int(unsafe.Sizeof(scenePbrRecord{})) {
		t.Fatalf("the material range is %d bytes, want the %d-byte record",
			materials[0].size, unsafe.Sizeof(scenePbrRecord{}))
	}
}

// The lighting a camera declares reaches the shader through its own pass's
// frame block, which is the only place a scene shader can read it.
func TestACamerasSunAndAmbientReachItsFrameBlock(t *testing.T) {
	descr := testCameraDescr()
	descr.SunDirection = m.Vec3{Y: -1}
	descr.SunColor = m.NewColorLinear(1, 1, 1, 1)
	descr.AmbientSky = m.NewColorLinear(0.2, 0.3, 0.4, 1)
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, descr)
		q.Box(0, At(0, 0, 0), testBoxColor)
	})
	h.frame()

	frames := h.backend.buffersBoundTo("sceneFrame")
	if len(frames) != 1 {
		t.Fatalf("sceneFrame was bound %d times, want once per draw", len(frames))
	}
	if frames[0].size != int(unsafe.Sizeof(sceneFrameBlock{})) {
		t.Fatalf("sceneFrame is bound as %d bytes, want the %d-byte block including its lighting",
			frames[0].size, unsafe.Sizeof(sceneFrameBlock{}))
	}
}
