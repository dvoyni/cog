package internal

import (
	"unsafe"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
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
	// AnimOffset indexes sceneAnim, or is SceneNoAnim when the draw animates
	// nothing, which is what skips the animation path entirely.
	AnimOffset uint32
	Flags      uint32
	// Joint is the model joint a plain-bound placement rides at full weight,
	// and is meaningful only under SCENE_PLAINJOINT. It spent the first of the
	// record's two spare words, which is what took the node joint out of every
	// vertex of the geometry: a mesh under nine animated nodes is one
	// conversion and nine instances rather than nine of each.
	//
	// It is a full 32 bits rather than the 16 a vertex attribute had. Nothing
	// but node count bounds how many plain joints a file claims, and they are
	// appended after every skin's, so a narrow field here would truncate a
	// plain index into a different bone with no error anywhere.
	Joint uint32
	// Mesh indexes the per-mesh record buffer, which carries the scale and bias
	// the instance's geometry decodes its two UV sets against. Slot 0 is the
	// reserved identity, so a custom-layout mesh and a standard mesh with no
	// UVs both name it and dequantise to a no-op.
	//
	// It spent the record's last spare word. sceneInstance is fully allocated
	// at 64 bytes: there is nothing left for the next thing that wants
	// per-instance data, and that next spender pays 128 bytes or a repack of
	// what is already here.
	Mesh uint32
}

// The instance flags. They are decided here rather than when their consumers
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
	// The flag is read only by the variant that declares the pose bindings at
	// all. A draw with no skin buffers takes a variant without them, so the
	// early return it selects is a second line of defence rather than the
	// mechanism: what removes the cost is that the code is not there.
	sceneNoSkin uint32 = 1 << 1
	// scenePlainJoint marks a placement bound to one joint at full weight — an
	// animated mesh node, which is how glTF authors a wheel, a door or a
	// propeller. The joint is in the record rather than in the vertices, so
	// the geometry under it is the geometry every other node referencing that
	// mesh draws.
	//
	// It is a flag inside the skinning path rather than a fifth variant, which
	// follows the precedent SCENE_NOSKIN already set. It is also cheaper than
	// the vertex form was: one influence's work instead of a four-iteration
	// loop that hit a zero-weight continue three times, and no attribute fetch
	// for a value the whole draw shares.
	//
	// It is mutually exclusive with sceneNoSkin by construction — packInstance
	// sets one or neither — because a plain-bound placement is a skinned draw.
	scenePlainJoint uint32 = 1 << 2
)

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
	// ViewDirection is xyz the constant world direction from a surface towards
	// the viewer and w a mix selector: 1 when that constant is the answer, 0
	// when the shader must difference against CameraPosition per fragment.
	// Only Perspective has a real eye, so only Perspective takes the 0; see
	// ViewDirection.
	ViewDirection m.Vec4
	// SunDirection is the sun's direction of travel, normalised, and zero when
	// the camera declared no sun. SunColor, AmbientSky and AmbientGround are
	// linear radiance with their intensities already premultiplied: it removes
	// a per-fragment multiply and costs nothing. Their w is spare.
	SunDirection  m.Vec4
	SunColor      m.Vec4
	AmbientSky    m.Vec4
	AmbientGround m.Vec4
	// LightCount bounds the shader's loop over Lights, the pass's punctual
	// lights after culling and the cap. The array is a fixed 16 - 768 bytes
	// inside the block - rather than runtime-sized, which the fixed cap is
	// what allows; pad takes it to the array's 16-byte alignment.
	LightCount uint32
	pad        [3]uint32
	Lights     [model.MaxLights]sceneLight
}

// packFrameLighting writes one camera's sun and hemispheric ambient into its
// frame block. Both stay per-camera fields rather than entries in the light
// array: packing the sun as a directional entry would cost an explicit
// discriminator and waste position, range and cone on it, and hemispheric
// ambient is normal-dependent rather than a direction, so it could never join
// the loop anyway.
func packFrameLighting(block sceneFrameBlock, descr scene.CameraDescr) sceneFrameBlock {
	if direction := descr.SunDirection.Normalize(); direction != (m.Vec3{}) {
		block.SunDirection = m.Vec4{X: direction.X, Y: direction.Y, Z: direction.Z}
		block.SunColor = radiance(descr.SunColor, descr.SunIntensity)
	}
	block.AmbientSky = radiance(descr.AmbientSky, descr.AmbientIntensity)
	block.AmbientGround = radiance(descr.AmbientGround, descr.AmbientIntensity)
	return block
}

// radiance premultiplies a linear colour by its intensity, where zero means 1.
// Intensity is unitless - radiance at one world unit - because targets are 8-bit
// sRGB with no tonemapping and no exposure control anywhere, so shading has to
// land in 0..1 directly.
func radiance(color m.Color, intensity float32) m.Vec4 {
	if intensity == 0 {
		intensity = 1
	}
	return m.Vec4{X: color.R * intensity, Y: color.G * intensity, Z: color.B * intensity}
}

// packInstance builds the instance record for one world matrix under one
// batch's animation, naming the per-mesh record slot its geometry decodes
// against.
func packInstance(world m.Mat4, anim types.AnimBinding, mesh uint32) sceneInstance {
	instance := sceneInstance{
		World0:     m.Vec4{X: world[0], Y: world[4], Z: world[8], W: world[12]},
		World1:     m.Vec4{X: world[1], Y: world[5], Z: world[9], W: world[13]},
		World2:     m.Vec4{X: world[2], Y: world[6], Z: world[10], W: world[14]},
		AnimOffset: anim.Offset,
		Mesh:       mesh,
	}
	// One arm or neither, which is what makes the two flags mutually exclusive
	// by construction rather than by a rule someone has to keep: a plain-bound
	// placement is a skinned draw, and an unskinned one has no joint to name.
	switch {
	case !anim.Skinned:
		instance.Flags |= sceneNoSkin
	case anim.Plain:
		instance.Flags |= scenePlainJoint
		instance.Joint = anim.Joint
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

// padToVec4 rounds the arena up to a whole vec4. The sceneAnim arena needs it
// because animOffset counts vec4s while the morph list packs two eight-byte
// entries into one, so an odd target count would leave the next block starting
// at an offset no instance can name.
func (a *arena) padToVec4() {
	if remainder := len(a.data) % 16; remainder != 0 {
		a.data = append(a.data, make([]byte, 16-remainder)...)
	}
}

// recordBytes reinterprets a record as the bytes uploaded for it. Every GPU
// target cog builds for is little-endian, so the in-memory layout is the wire
// layout — the same reinterpretation canvas's sprite instances use.
func recordBytes[T any](record *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(record)), unsafe.Sizeof(*record))
}
