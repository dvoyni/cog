package ecsscene

import "github.com/dvoyni/cog/bundles/ecsscene/internal"

// Model draws a glTF file, or one scene or node inside it.
//
// Ref.Path is the storage path model loads, held as the string it is. The
// first frame that names a path enqueues its load and draws nothing, and a path
// that never loads is reported, once.
type Model = internal.Model

// Mesh draws a mesh model already holds, named by the ref its bake returned.
// It is pointer-free.
type Mesh = internal.Mesh

// Animation is the clips a Model Entity blends this frame. It is optional and
// has no effect on a Mesh.
//
// Nothing in this package advances clip time. Time is the game's, written into
// the plays by a game System, because model is stateless about animation and
// so is ecsscene.
type Animation = internal.Animation

// Params are gfx parameters bound on top of whatever the draw's material binds.
// It is optional.
//
// They ride on the draw, so gfx lays them by name over every tag's params, the
// material's numbers among them, a Model's and a Mesh's alike — so
// gfx.ColorParam("baseColorFactor", c) tints either.
type Params = internal.Params

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
type Material = internal.Material

// Light is a punctual light, model's own descriptor with a layer mask. It is
// pointer-free.
//
// Descr.Position and Descr.Direction are ignored: the light stands at its
// Entity's m.Transform's position, and a spot's direction is that Transform's
// rotation applied to -Z, which is the way m.LookAt faces. Descr.Kind says
// point or spot, and every other zero is model's default: Intensity zero is 1,
// Range zero is infinite, OuterCone zero is pi/4, and InnerCone zero is a real
// value.
type Light = internal.Light

// Camera is a camera. Its placement is its Entity's m.Transform's position and
// rotation; its Scale is ignored.
//
// The fields keep scene's camera's names and meanings, with Passes held as a
// List. An empty Passes is one default pass. Two Cameras with one ID keep the
// first and report the second.
type Camera = internal.Camera

// DebugBox is a solid box of Size, centred on its Entity.
type DebugBox = internal.DebugBox

// DebugSphere is a solid sphere of Radius, centred on its Entity. Its
// tessellation is fixed: 16 segments by 12 rings, 352 triangles. A sphere that
// needs to be smoother or cheaper is a Mesh.
type DebugSphere = internal.DebugSphere

// DebugPlane is a one-sided rectangle on the plane of the points p with
// Normal·p + D = 0, centred at -D along the normalised Normal and facing along
// it. Size.X runs along world +X projected onto the plane, or along +Z where
// the Normal is along X, and Size.Y across it; so a ground plane, Normal +Y,
// spans Size.X along X and Size.Y along Z. A Transform rotation turns it
// further.
type DebugPlane = internal.DebugPlane

// DebugLine is a line from From to To, drawn as a box of Width by Width across
// it. Width is a size in the Entity's space, so a distant line thins out on
// screen the way any object does; a line of a fixed number of pixels is not
// this. With an identity Transform, From and To are world positions.
type DebugLine = internal.DebugLine

// DebugWireBox is the twelve edges of a box of Size centred on its Entity, each
// a line of Width. Edges run half a Width past each corner, so the corners
// close.
type DebugWireBox = internal.DebugWireBox
