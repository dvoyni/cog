package scene

import (
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// sceneInstance is the per-instance record every scene draw reads through
// sceneInstances, and it is 64 bytes on purpose.
//
// It carries no normal matrix. Under non-uniform scale a normal transformed by
// world is wrong, and scene generates that case itself the moment a debug line
// becomes a stretched box — but a second 3 x vec4 would take the record to 128
// bytes and charge every line for a case it does not have. So the packer sets
// SCENE_NONUNIFORM instead and the shader takes the inverse-transpose for those
// instances alone, branch-uniform across the whole instance.
//
// Field order and size must match SceneInstance in builtin/scene/scene.wgsl.
type sceneInstance struct {
	// World0..World2 are the rows of the 4x3 world matrix: row i of the packed
	// matrix, translation in w. Three rows rather than a mat4x4 because the
	// fourth row of an affine transform is known.
	World0, World1, World2 m.Vec4
	// AnimOffset indexes sceneAnim, or is sceneNoAnim when the draw animates
	// nothing, which is what skips the animation path entirely.
	AnimOffset uint32
	Flags      uint32
	// Spare is the record's eight unspent bytes, kept named so the next thing
	// that needs per-instance data can see what it is spending.
	Spare [2]uint32
}

// The instance flags. Both are decided here rather than when their consumers
// land, because the record's size and the null-skin bind group both follow from
// them.
const (
	// sceneNonUniform marks an instance whose packed matrix does not scale
	// uniformly, and is the shader's signal to take an inverse-transpose for
	// its normals.
	sceneNonUniform uint32 = 1 << 0
	// sceneNoSkin marks a draw with no skin of its own — every buffer-built
	// mesh and every debug shape. Riding the rest-frame path instead would be
	// correct, but it charges a procedural terrain mesh a per-vertex pose fetch
	// for a guaranteed identity.
	//
	// It is set on every draw scene can make today, because every draw is
	// buffer-built. The shared null-skin bind group it exists to select — one
	// identity pose row and one-element inverse-bind, normal-matrix and
	// morph-delta arrays — lands with the group 2 bindings themselves: a bind
	// group can only fill bindings that exist, and no shader declares them yet.
	sceneNoSkin uint32 = 1 << 1
)

// sceneNoAnim is the animOffset of an instance that animates nothing.
const sceneNoAnim uint32 = ^uint32(0)

// nonUniformTolerance is the relative spread between the packed matrix's three
// basis lengths that still counts as uniform. It is relative because the test
// has to hold for a millimetre-scale prop and a kilometre-scale terrain alike.
const nonUniformTolerance = 1e-6

// sceneFrameBlock is one pass's view of the world, bound once per pass through
// sceneFrame. It carries view and projection separately as well as their
// product because a shader that needs view-space depth cannot recover them from
// the product.
//
// Field order and size must match SceneFrame in builtin/scene/scene.wgsl.
type sceneFrameBlock struct {
	View           m.Mat4
	Projection     m.Mat4
	ViewProjection m.Mat4
	CameraPosition m.Vec4
}

// sceneMaterialRecord is the bundled material's per-batch record, bound as a
// range of the frame's material arena. In this form it carries only a base
// colour factor: the tracer is deliberately unlit, and the BRDF and its texture
// slots land with the bundled PBR.
//
// Field order and size must match ScenePbrMaterial in
// builtin/scene/scene.wgsl.
type sceneMaterialRecord struct {
	BaseColorFactor m.Vec4
}

// packInstance builds the instance record for one world matrix. Everything the
// record says about animation is the buffer-built answer — no anim, no skin —
// because that is every draw scene can make until models land.
func packInstance(world m.Mat4) sceneInstance {
	instance := sceneInstance{
		World0:     m.Vec4{X: world[0], Y: world[4], Z: world[8], W: world[12]},
		World1:     m.Vec4{X: world[1], Y: world[5], Z: world[9], W: world[13]},
		World2:     m.Vec4{X: world[2], Y: world[6], Z: world[10], W: world[14]},
		AnimOffset: sceneNoAnim,
		Flags:      sceneNoSkin,
	}
	if !uniformScale(world) {
		instance.Flags |= sceneNonUniform
	}
	return instance
}

// uniformScale reports whether a matrix scales its three basis vectors by the
// same factor. It compares squared lengths, so it costs three dot products and
// no square roots, and it is relative so that scale itself does not decide the
// answer.
func uniformScale(matrix m.Mat4) bool {
	lengths := [3]float32{
		matrix[0]*matrix[0] + matrix[1]*matrix[1] + matrix[2]*matrix[2],
		matrix[4]*matrix[4] + matrix[5]*matrix[5] + matrix[6]*matrix[6],
		matrix[8]*matrix[8] + matrix[9]*matrix[9] + matrix[10]*matrix[10],
	}
	low, high := lengths[0], lengths[0]
	for _, length := range lengths[1:] {
		low, high = min(low, length), max(high, length)
	}
	return high-low <= nonUniformTolerance*high
}

// arena is one frame's staging bytes for one storage binding. Records are
// appended into it and bound back out as ranges, which is how a draw addresses
// its own record without an index anyone has to agree on across the update and
// render threads.
//
// It keeps its backing across frames: a reset truncates, so a steady frame
// allocates nothing after the first.
type arena struct {
	data []byte
}

// reset empties the arena for a new frame without giving up its backing.
func (a *arena) reset() { a.data = a.data[:0] }

// bytes returns the arena's contents, valid until the next reset.
func (a *arena) bytes() []byte { return a.data }

// beginRange pads the arena up to a bindable offset and returns it. A storage
// binding's offset must be a multiple of gfx.StorageAlignment, so anything
// bound as its own range starts here.
func (a *arena) beginRange() int {
	if remainder := len(a.data) % gfx.StorageAlignment; remainder != 0 {
		a.data = append(a.data, make([]byte, gfx.StorageAlignment-remainder)...)
	}
	return len(a.data)
}

// appendRecord appends one bindable record and returns its offset.
func (a *arena) appendRecord[T any](record *T) int {
	offset := a.beginRange()
	a.data = append(a.data, recordBytes(record)...)
	return offset
}

// appendElement appends one element of an array binding, packed tight against
// the element before it: an array's elements are addressed by index inside one
// bound range, not bound separately.
func (a *arena) appendElement[T any](element *T) int {
	offset := len(a.data)
	a.data = append(a.data, recordBytes(element)...)
	return offset
}

// recordBytes reinterprets a record as the bytes uploaded for it. Every GPU
// target cog builds for is little-endian, so the in-memory layout is the wire
// layout — the same reinterpretation canvas's sprite instances use.
func recordBytes[T any](record *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(record)), unsafe.Sizeof(*record))
}
