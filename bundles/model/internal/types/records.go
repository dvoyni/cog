package types

import (
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

// The bundled shader's storage bindings, by the names its WGSL declares them
// under. A renderer binds every one of them through these rather than a string
// of its own, so a rename in the shader is a rename here and nowhere else.
const (
	// BindingSceneFrame is one pass's FrameBlock, bound as a range.
	BindingSceneFrame = "sceneFrame"
	// BindingSceneInstances is one pass's slice of Instance records, bound as
	// a range so that instance_index stays pass-relative.
	BindingSceneInstances = "sceneInstances"
	// BindingSceneAnim is the frame's whole animation arena, which AppendAnim
	// writes and an instance's AnimOffset indexes in vec4s.
	BindingSceneAnim = "sceneAnim"
	// BindingSceneMeshes is the frame's whole SceneMesh arena, which an
	// instance's Mesh indexes in records.
	BindingSceneMeshes = "sceneMeshes"
	// BindingScenePoses and BindingSceneSkinJoints are a model's two durable
	// pose buffers, and BindingSceneMorphDeltas its delta buffer: group 2,
	// bound only where the draw's variant declares them.
	BindingScenePoses       = "scenePoses"
	BindingSceneSkinJoints  = "sceneSkinJoints"
	BindingSceneMorphDeltas = "sceneMorphDeltas"
)

// Instance is the per-instance record every draw of the bundled shader reads
// through sceneInstances, and it is 64 bytes on purpose.
//
// It carries no normal matrix. Under non-uniform scale a normal transformed by
// world is wrong, and scene generates that case itself the moment a debug line
// becomes a stretched box - but a second 3 x vec4 would take the record to 128
// bytes and charge every line for a case it does not have. So the packer sets
// SCENE_NONUNIFORM instead and the shader takes the inverse-transpose for those
// instances alone, branch-uniform across the whole instance.
//
// Field order and size must match SceneInstance in builtin/scene/instance.wgsl.
type Instance struct {
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
	// It spent the record's last spare word. Instance is fully allocated at 64
	// bytes: there is nothing left for the next thing that wants per-instance
	// data, and that next spender pays 128 bytes or a repack of what is already
	// here.
	Mesh uint32
}

// SceneNoAnim is the AnimOffset of an instance that animates nothing, and what
// AppendAnim returns for a draw with no plays and no morph targets.
const SceneNoAnim uint32 = ^uint32(0)

// The instance flags, named after the WGSL constants the shader tests them by.
// They are decided here rather than when their consumers land, because the
// record's size and the null-skin bind group both follow from them.
const (
	// SceneNonUniform marks an instance whose packed matrix does not scale
	// uniformly, and is the shader's signal to take an inverse-transpose for
	// its normals.
	SceneNonUniform uint32 = 1 << 0
	// SceneNoSkin marks a draw with no skin of its own - every buffer-built
	// mesh and every debug shape. Riding the rest-frame path instead would be
	// correct, but it charges a procedural terrain mesh a per-vertex pose fetch
	// for a guaranteed identity.
	//
	// The flag is read only by the variant that declares the pose bindings at
	// all. A draw with no skin buffers takes a variant without them, so the
	// early return it selects is a second line of defence rather than the
	// mechanism: what removes the cost is that the code is not there.
	SceneNoSkin uint32 = 1 << 1
	// ScenePlainJoint marks a placement bound to one joint at full weight - an
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
	// It is mutually exclusive with SceneNoSkin by construction - PackInstance
	// sets one or neither - because a plain-bound placement is a skinned draw.
	ScenePlainJoint uint32 = 1 << 2
)

// InstanceAnim is what one instance record says about animation: the offset
// of its sceneAnim block, whether the placement is skinned at all, and the one
// joint it rides when it is plain-bound. A renderer keeps it per batch, since
// the instances of one call share the draw's animation.
type InstanceAnim struct {
	// Offset is what AppendAnim returned for the draw, or SceneNoAnim.
	Offset uint32
	// Skinned is false for every buffer-built mesh and every debug shape, and
	// for a model primitive no clip can move. Riding the free rest-frame path
	// instead would be correct, but it charges a procedural terrain mesh - the
	// highest-vertex-count thing a renderer can be handed - a per-vertex pose
	// fetch and TRS blend for a guaranteed identity.
	Skinned bool
	// Joint is the model joint a plain-bound placement rides, and Plain says
	// it is one: joint 0 is a joint like any other, and every buffer-built
	// draw's binding is the zero value. A plain binding implies skinned.
	Joint uint32
	Plain bool
}

// nonUniformTolerance is the relative spread between the packed matrix's three
// basis lengths that still counts as uniform. It is relative because the test
// has to hold for a millimetre-scale prop and a kilometre-scale terrain alike.
const nonUniformTolerance = 1e-6

// PackInstance builds the instance record for one world matrix under one
// batch's animation, naming the per-mesh record slot its geometry decodes
// against.
func PackInstance(world m.Mat4, anim InstanceAnim, mesh uint32) Instance {
	instance := Instance{
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
		instance.Flags |= SceneNoSkin
	case anim.Plain:
		instance.Flags |= ScenePlainJoint
		instance.Joint = anim.Joint
	}
	if !uniformScale(world) {
		instance.Flags |= SceneNonUniform
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

// FrameBlock is one pass's view of the world, bound once per pass through
// sceneFrame. It carries view and projection separately as well as their
// product because a shader that needs view-space depth cannot recover them from
// the product.
//
// A renderer fills the five view fields itself, from its own camera, and
// PackFrameLighting fills the rest.
//
// Field order and size must match SceneFrame in builtin/scene/frame.wgsl.
type FrameBlock struct {
	View           m.Mat4
	Projection     m.Mat4
	ViewProjection m.Mat4
	CameraPosition m.Vec4
	// ViewDirection is xyz the constant world direction from a surface towards
	// the viewer and w a mix selector: 1 when that constant is the answer, 0
	// when the shader must difference against CameraPosition per fragment.
	// Only Perspective has a real eye, so only Perspective takes the 0; see
	// m.ViewDirection.
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
	Lights     [MaxLights]Light
}

// FrameLighting is a camera's sun and hemispheric ambient, under the names a
// camera declares them by. A renderer copies them from its own camera at the
// call site, so no camera has to take this shape.
//
// Every zero is a default: a zero SunDirection is no sun, and a zero intensity
// means 1.
type FrameLighting struct {
	SunDirection m.Vec3 // direction of travel; zero means no sun
	SunColor     m.Color
	SunIntensity float32 // zero means 1

	AmbientSky       m.Color
	AmbientGround    m.Color
	AmbientIntensity float32 // zero means 1
}

// PackFrameLighting writes one pass's lighting into its frame block: the sun,
// the hemispheric ambient, and the lights selection holds with their count.
// The view fields are the renderer's and are left as they came in.
//
// The sun and the ambient stay per-camera fields rather than entries in the
// light array: packing the sun as a directional entry would cost an explicit
// discriminator and waste position, range and cone on it, and hemispheric
// ambient is normal-dependent rather than a direction, so it could never join
// the loop anyway.
func PackFrameLighting(block FrameBlock, lighting FrameLighting, selection *LightSelection) FrameBlock {
	if direction := lighting.SunDirection.Normalize(); direction != (m.Vec3{}) {
		block.SunDirection = m.Vec4{X: direction.X, Y: direction.Y, Z: direction.Z}
		block.SunColor = radiance(lighting.SunColor, lighting.SunIntensity)
	}
	block.AmbientSky = radiance(lighting.AmbientSky, lighting.AmbientIntensity)
	block.AmbientGround = radiance(lighting.AmbientGround, lighting.AmbientIntensity)
	block.LightCount = uint32(selection.count)
	block.Lights = selection.lights
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

// AnimMorph is the morph half of one draw's sceneAnim block: the primitive's
// addressing constants and the sparse list of targets SelectMorphTargets kept.
// Its zero value is no morph at all.
type AnimMorph struct {
	Binding MorphBinding
	Targets []SceneMorphWeight
}

// AppendAnim writes one draw's sceneAnim block onto dst and returns the grown
// slice with the AnimOffset an instance carries: the block's position in
// vec4s, or SceneNoAnim, with dst untouched, when the draw animates nothing.
//
// The block is the header, then the plays, then the morph list, padded to a
// whole vec4. The padding is part of the layout: two morph entries share one
// vec4 and AnimOffset counts vec4s, so an odd target count would otherwise
// leave the next block starting at an offset no instance can name. dst is
// padded up to a vec4 first for the same reason, which is a no-op for a slice
// only this function has written.
//
// The block is indirect because sceneInstances is bound once per pass and
// shared by every draw in it: a fixed per-instance record would have to be
// sized for the worst case and put a ~320 byte tax on every debug line against
// about 48 bytes of content.
//
// A skinned draw appends one block per call and a morphed one a block per
// primitive, because the three morph words are per-primitive constants. Putting
// them in the material's uniform block would remove that duplication exactly,
// and was rejected: it would put geometry constants into a block gfx packs on
// the render thread while a renderer records on the update thread.
func AppendAnim(dst []byte, plays []ScenePlayRecord, morph AnimMorph) ([]byte, uint32) {
	if len(plays) == 0 && len(morph.Targets) == 0 {
		return dst, SceneNoAnim
	}
	dst = padToVec4(dst)
	offset := uint32(len(dst) / 16)
	header := SceneAnimHeader{
		PlayCount:   uint32(len(plays)),
		TargetCount: uint32(len(morph.Targets)),
		MorphBase:   morph.Binding.Base,
		MorphStride: morph.Binding.Stride,
	}
	dst = append(dst, recordSliceBytes(unsafe.Slice(&header, 1))...)
	dst = append(dst, recordSliceBytes(plays)...)
	dst = append(dst, recordSliceBytes(morph.Targets)...)
	return padToVec4(dst), offset
}

// padToVec4 rounds a byte slice up to a whole vec4 with zeros.
func padToVec4(data []byte) []byte {
	if remainder := len(data) % 16; remainder != 0 {
		var zeros [16]byte
		data = append(data, zeros[:16-remainder]...)
	}
	return data
}

// The sizes of the records the shader reads, in bytes. They are the Go
// structs' sizes because the Go structs are what a renderer writes; the WGSL
// side of the same contract is asserted where the shader is reflected. Binding
// ranges are built from them.
const (
	InstanceSize         = int(unsafe.Sizeof(Instance{}))
	FrameBlockSize       = int(unsafe.Sizeof(FrameBlock{}))
	LightSize            = int(unsafe.Sizeof(Light{}))
	SceneMeshSize        = int(unsafe.Sizeof(SceneMesh{}))
	SceneAnimHeaderSize  = int(unsafe.Sizeof(SceneAnimHeader{}))
	ScenePlayRecordSize  = int(unsafe.Sizeof(ScenePlayRecord{}))
	SceneMorphWeightSize = int(unsafe.Sizeof(SceneMorphWeight{}))
)
