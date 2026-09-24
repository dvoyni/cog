package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

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

// DebugBox is a solid box of Size, centred on its Entity.
type DebugBox struct {
	Size  m.Vec3
	Color m.Color
	// Layers is the shape's layer mask, whose zero reads as every layer.
	Layers LayerMask
}

// DebugSphere is a solid sphere of Radius, centred on its Entity. Its
// tessellation is fixed: 16 segments by 12 rings, 352 triangles. A sphere that
// needs to be smoother or cheaper is a Mesh.
type DebugSphere struct {
	Radius float32
	Color  m.Color
	Layers LayerMask
}

// DebugPlane is a one-sided rectangle on the plane of the points p with
// Normal·p + D = 0, centred at -D along the normalised Normal and facing along
// it. Size.X runs along world +X projected onto the plane, or along +Z where
// the Normal is along X, and Size.Y across it; so a ground plane, Normal +Y,
// spans Size.X along X and Size.Y along Z. A Transform rotation turns it
// further.
type DebugPlane struct {
	Normal m.Vec3
	D      float32
	Size   m.Vec2
	Color  m.Color
	Layers LayerMask
}

// DebugLine is a line from From to To, drawn as a box of Width by Width across
// it. Width is a size in the Entity's space, so a distant line thins out on
// screen the way any object does; a line of a fixed number of pixels is not
// this. With an identity Transform, From and To are world positions.
type DebugLine struct {
	From, To m.Vec3
	Width    float32
	Color    m.Color
	Layers   LayerMask
}

// DebugWireBox is the twelve edges of a box of Size centred on its Entity, each
// a line of Width. Edges run half a Width past each corner, so the corners
// close.
type DebugWireBox struct {
	Size   m.Vec3
	Width  float32
	Color  m.Color
	Layers LayerMask
}
