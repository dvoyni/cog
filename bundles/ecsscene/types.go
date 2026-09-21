package ecsscene

import (
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Model draws a glTF file, or one scene or node inside it.
//
// Ref.Path is the storage path scene loads, held as the string it is. Loading
// is scene's: the first frame that names a path enqueues its load and draws
// nothing, and a path that never loads is reported by scene, once.
type Model struct {
	Ref scene.ModelRef
	// Layers is scene's own mask, whose zero reads as every layer.
	Layers scene.LayerMask
}

// Mesh draws a mesh scene already holds, named by the ref BakeMesh returned.
// It is pointer-free.
type Mesh struct {
	Ref scene.MeshRef
	// Bounds is the local bounding sphere, xyz centre and w radius; zero is the
	// mesh's own baked sphere.
	Bounds m.Vec4
	// Layers is scene's own mask, whose zero reads as every layer.
	Layers    scene.LayerMask
	NeverCull bool
}

// Animation is the clips a Model Entity blends this frame. It is optional and
// has no effect on a Mesh.
//
// Nothing in this package advances clip time. Time is the game's, written into
// the plays by a game System, because scene is stateless about animation and so
// is the binding.
type Animation struct {
	// Plays is a fixed array at scene's own cap. A play whose Clip is empty is
	// an unused slot and is skipped, so how many plays there are is a property
	// of the data rather than a count that can disagree with it.
	Plays [MaxPlays]scene.ClipPlay
}

// Params are gfx parameters bound on top of whatever the draw's material binds.
// It is optional.
//
// On a Mesh they are the draw's own params. On a Model they are the override
// params, which merge by name over the file's materials — so
// gfx.ColorParam("baseColorFactor", c) tints a glTF model.
type Params struct {
	Values m.List[gfx.ParameterDescr]
}

// Material replaces what a draw is shaded with, one entry per pass tag. It is
// optional, and there is no cap on its tags.
//
// An absent Material is no material: the bundled PBR for a Mesh, and the file's
// own materials for a Model. Presence is what says otherwise: a Material whose
// Tags are empty is handed to scene as an empty non-nil scene.Material, which
// scene documents as a material serving no pass.
type Material struct {
	Tags m.List[MaterialTag]
}

// Light is a punctual light. Its position is its Entity's m.Transform's, and a
// spot's direction is that Transform's rotation applied to -Z, which is the way
// m.LookAt faces. It is pointer-free.
//
// Every zero is scene's default: Intensity zero is 1, Range zero is infinite,
// OuterCone zero is pi/4, and InnerCone zero is a real value.
type Light struct {
	Kind      scene.LightKind
	Color     m.Color
	Intensity float32
	Range     float32
	InnerCone float32
	OuterCone float32
	Layers    scene.LayerMask
}

// Camera is a camera. Its placement is its Entity's m.Transform's position and
// rotation; its Scale is ignored, as scene ignores it.
//
// The fields are scene.CameraDescr's, with its Passes held as a List. An empty
// Passes is scene's one default pass. Two Cameras with one ID are left to
// scene, which keeps the first and reports the second.
type Camera struct {
	ID         scene.CameraID
	Projection scene.ProjectionKind
	FovY       float32
	Height     float32
	Shear      float32
	Near, Far  float32

	CullMask scene.LayerMask

	SunDirection m.Vec3
	SunColor     m.Color
	SunIntensity float32

	AmbientSky       m.Color
	AmbientGround    m.Color
	AmbientIntensity float32

	Passes m.List[scene.Pass]
}

// MaxPlays is how many clips one Animation blends. It is scene's own cap:
// scene drops a fifth play by lowest weight and reports it, so a larger array
// here would buy a report and no animation.
const MaxPlays = 4

// MaterialTag is one pass tag of a Material Component: scene.MaterialTag with
// its gfx.MaterialDescr spelled out as the three things it is made of.
//
// It cannot hold a gfx.MaterialDescr, because a descriptor keeps its params as
// a bare slice, which a Component may not hold. The recording System rebuilds
// the descriptor every draw from these fields, in scratch, and scene copies it
// into its frame arenas at record.
type MaterialTag struct {
	// Tag is the pass this entry serves; zero reads as scene.TagForward.
	Tag    scene.PassTag
	Shader gfx.ShaderDescr
	State  gfx.MaterialState
	Params m.List[gfx.ParameterDescr]
}
