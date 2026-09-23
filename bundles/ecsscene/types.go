package ecsscene

import (
	"github.com/dvoyni/cog/bundles/ecsscene/internal/types"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The Components wrap model's values rather than being them: ecsscene never
// registers a model type, because a Go type has one Store, and registering
// model.MeshRef here would claim it for every other plugin.

// Model draws a glTF file, or one scene or node inside it.
//
// Ref.Path is the storage path model loads, held as the string it is. The
// first frame that names a path enqueues its load and draws nothing, and a path
// that never loads is reported, once.
type Model struct {
	Ref model.ModelRef
	// Layers is the Entity's layer mask, whose zero reads as every layer.
	Layers LayerMask
	// The tail padding is spelled out, because the load System watches Model
	// for Changed, and a change is a difference in bytes.
	_ [4]byte
}

// Mesh draws a mesh model already holds, named by the ref its bake returned.
// It is pointer-free.
type Mesh struct {
	Ref model.MeshRef
	// Bounds is the local bounding sphere, xyz centre and w radius; zero is the
	// mesh's own baked sphere.
	Bounds m.Vec4
	// Layers is the Entity's layer mask, whose zero reads as every layer.
	Layers    LayerMask
	NeverCull bool
	// The tail padding is spelled out, because the load System watches Mesh
	// for Changed, and a change is a difference in bytes.
	_ [3]byte
}

// Animation is the clips a Model Entity blends this frame. It is optional and
// has no effect on a Mesh.
//
// Nothing in this package advances clip time. Time is the game's, written into
// the plays by a game System, because model is stateless about animation and
// so is ecsscene.
type Animation struct {
	// Plays is a fixed array at model's own cap, model.MaxClipPlays: a larger
	// array would buy a report and no animation. A play whose Clip is empty is
	// an unused slot and is skipped, so how many plays there are is a property
	// of the data rather than a count that can disagree with it.
	Plays [model.MaxClipPlays]model.ClipPlay
}

// Params are gfx parameters bound on top of whatever the draw's material binds.
// It is optional.
//
// They ride on the draw, so gfx lays them by name over every tag's params, the
// material's numbers among them, a Model's and a Mesh's alike — so
// gfx.ColorParam("baseColorFactor", c) tints either.
type Params struct {
	Values m.List[gfx.ParameterDescr]
}

// Material changes what a draw is shaded with, one entry per pass tag, laid
// over what the file provides rather than put in its place. It is optional, and
// there is no cap on its tags.
//
// For each primitive of a Model, each tag resolves to: its Shader, or model's
// default scene shader where it names none, under the defines the primitive's
// geometry needs; the primitive's own params - its textures, samplers and
// numbers - overlaid by name with the default scene shader's params and then
// its own; and its State, or the file's where it names none. A Mesh's "file" is the bundled PBR's: white and
// flat in every slot, opaque, single-sided, white paint. gfx binds params by
// name and ignores one no binding declares, so a shader that reads none of the
// file's simply replaces it.
//
// An absent Material is no material: the default scene shader over the file's
// own materials for a Model, and over the bundled PBR's for a Mesh. Presence is
// what says otherwise: a Material whose Tags are empty is a material serving no
// pass, so its Entity draws in none.
type Material struct {
	Tags m.List[MaterialTag]
}

// Light is a punctual light, model's own descriptor with a layer mask. It is
// pointer-free.
//
// Descr.Position and Descr.Direction are ignored: the light stands at its
// Entity's m.Transform's position, and a spot's direction is that Transform's
// rotation applied to -Z, which is the way m.LookAt faces. Descr.Kind says
// point or spot, and every other zero is model's default: Intensity zero is 1,
// Range zero is infinite, OuterCone zero is pi/4, and InnerCone zero is a real
// value.
type Light struct {
	Descr  model.LightDescr
	Layers LayerMask
}

// Camera is a camera. Its placement is its Entity's m.Transform's position and
// rotation; its Scale is ignored.
//
// The fields keep scene's camera's names and meanings, with Passes held as a
// List. An empty Passes is one default pass. Two Cameras with one ID keep the
// first and report the second.
type Camera struct {
	ID         CameraID
	Projection ProjectionKind
	FovY       float32 // Perspective: the literal vertical field of view, radians
	Height     float32 // Orthographic and Oblique: world units across the target's height
	// Shear is the Oblique gain: how far one world unit of view-space depth
	// rides up the screen, in the units Height measures. 1 is cavalier, 0.5
	// cabinet, 0 exactly Orthographic. Perspective and Orthographic do not
	// read it.
	Shear     float32
	Near, Far float32 // both required; a zero in either is a reported error

	CullMask LayerMask // zero reads as LayersAll

	SunDirection m.Vec3 // direction of travel; zero means no sun
	SunColor     m.Color
	SunIntensity float32 // zero means 1

	AmbientSky       m.Color
	AmbientGround    m.Color
	AmbientIntensity float32 // zero means 1

	Passes m.List[Pass]
}

// ecsscene's own camera, layer and pass vocabulary. Each keeps scene's name,
// shape and zero-value meaning, so a game moving between the two renderers
// rewrites an import and nothing else, and neither renderer names the other's.
// LightKind is model's, and projection maths is libs/m's.

// CameraID orders a camera among every other pass in the frame, and is the
// default gfx.Order for the passes it emits. It is a defined type over
// gfx.Order rather than an alias because it carries meaning the order does not:
// it is also a camera's identity, and two Cameras with one ID are an error.
type CameraID = types.CameraID

// ProjectionKind selects how a camera flattens the world: Perspective,
// Orthographic, or Oblique, which is Orthographic with view-space depth
// sheared into screen-up by the camera's Shear.
type ProjectionKind = types.ProjectionKind

const (
	Perspective  = types.Perspective
	Orthographic = types.Orthographic
	Oblique      = types.Oblique
)

// PassTag names what a pass is for, and selects which of a Material's tags
// serves it.
type PassTag = types.PassTag

// TagForward is the pass every lit and unlit surface draws into, and what an
// empty PassTag means.
const TagForward = types.TagForward

// Pass is one render pass a camera emits. Its zero value is the forward pass
// into the screen with automatic depth, which is what the default pass is.
type Pass = types.Pass

// LayerMask selects which cameras see a drawn Entity. A camera draws an Entity
// iff its Layers & the camera's CullMask is non-zero. Zero reads as LayersAll
// on both sides.
type LayerMask = types.LayerMask

// LayersAll is every layer, which is also what a mask nobody wrote means.
const LayersAll = types.LayersAll

// MaterialTag is one pass tag of a Material Component: a gfx material spelled
// out as the three things it is made of, each laid over what the draw's file
// provides. A zero Shader and a zero State are unset, not values: the draw
// keeps the default scene shader and the file's state. So a tag cannot name the
// zero state, gfx.StateOverlay2D.
//
// It cannot hold a gfx.MaterialDescr, because a descriptor keeps its params as
// a bare slice, which a Component may not hold. The recording System rebuilds
// the descriptor from these fields, in scratch.
type MaterialTag struct {
	// Tag is the pass this entry serves; zero reads as TagForward.
	Tag PassTag
	// Shader is resolved under the draw's SCENE_SKIN and SCENE_MORPH, which
	// the draw's geometry decides; zero is the default scene shader.
	Shader gfx.ShaderDescr
	// State is the pipeline state; zero is the file's.
	State gfx.MaterialState
	// Params are overlaid by name on the file's and the default scene
	// shader's.
	Params m.List[gfx.ParameterDescr]
}
