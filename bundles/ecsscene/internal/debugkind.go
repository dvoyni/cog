package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The two Materials every debug shape draws with, both through the debug
// shader, builtin/ecsscene/debug.wgsl in ecsscene's own mount: opaque, and
// blended for a Color whose alpha is below 1. Every shape of one kind holds the same value,
// so ecsscene keys them to one material and the per-shape colour rides in
// Params, as the bundled material's own paint does.
var (
	debugShader         = gfx.ShaderWithResource(debugShaderPath)
	debugOpaqueMaterial = Material{Tags: m.NewList(MaterialTag{
		Tag: TagForward, Shader: debugShader, State: gfx.StateOpaque3D(),
	})}
	debugBlendedMaterial = Material{Tags: m.NewList(MaterialTag{
		Tag: TagForward, Shader: debugShader, State: gfx.StateTransparent3D(),
	})}
)

// The five kinds.
var (
	debugBoxKind = debugKind[DebugBox]{
		geometry: appendDebugBox,
		paint:    func(s DebugBox) (m.Color, LayerMask) { return s.Color, s.Layers },
		form: func(s DebugBox) DebugBox {
			s.Color, s.Layers = m.Color{}, 0
			return s
		},
	}
	debugSphereKind = debugKind[DebugSphere]{
		geometry: appendDebugSphere,
		paint:    func(s DebugSphere) (m.Color, LayerMask) { return s.Color, s.Layers },
		form: func(s DebugSphere) DebugSphere {
			s.Color, s.Layers = m.Color{}, 0
			return s
		},
	}
	debugPlaneKind = debugKind[DebugPlane]{
		geometry: appendDebugPlane,
		paint:    func(s DebugPlane) (m.Color, LayerMask) { return s.Color, s.Layers },
		form: func(s DebugPlane) DebugPlane {
			s.Color, s.Layers = m.Color{}, 0
			return s
		},
	}
	debugLineKind = debugKind[DebugLine]{
		geometry: appendDebugLine,
		paint:    func(s DebugLine) (m.Color, LayerMask) { return s.Color, s.Layers },
		form: func(s DebugLine) DebugLine {
			s.Color, s.Layers = m.Color{}, 0
			return s
		},
	}
	debugWireBoxKind = debugKind[DebugWireBox]{
		geometry: appendDebugWireBox,
		paint:    func(s DebugWireBox) (m.Color, LayerMask) { return s.Color, s.Layers },
		form: func(s DebugWireBox) DebugWireBox {
			s.Color, s.Layers = m.Color{}, 0
			return s
		},
	}
)

// debugShape is every debug shape Component.
type debugShape interface {
	DebugBox | DebugSphere | DebugPlane |
		DebugLine | DebugWireBox
}

// debugBaked is one shape Entity whose mesh is baked: the ref, and the shape
// as it was last baked and painted, which is what the change System compares
// a record's value against.
type debugBaked[T debugShape] struct {
	ref   model.MeshRef
	shape T
}

// debugPending is one shape the bake System found with no Mesh.
type debugPending[T debugShape] struct {
	entity ecs.Entity
	shape  T
}

// debugScratch is one shape kind's state, shared by its two Systems: every
// baked Entity's ref, which a despawn record has no Mesh left to give back,
// and the backings a run builds geometry in. It is a resource the plugin
// owns, so it is in both Systems' lock sets.
type debugScratch[T debugShape] struct {
	baked    map[ecs.Entity]debugBaked[T]
	pending  []debugPending[T]
	vertices []model.Vertex
	indices  []uint32
}

func newDebugScratch[T debugShape]() *debugScratch[T] {
	return &debugScratch[T]{baked: map[ecs.Entity]debugBaked[T]{}}
}

// debugUnbaked is the bake System's Query: a shape with no Mesh.
type debugUnbaked[T debugShape] struct {
	Shape T
	_     ecs.Without[Mesh]
}

// debugDrawn is the three Components a shape adds and owns, reached through
// one accessor each.
type debugDrawn struct {
	meshes    *ecs.Set[Mesh]
	params    *ecs.Set[Params]
	materials *ecs.Set[Material]
}

// paint writes what draws a baked shape: its Mesh at its Layers, its Color as
// the Params the debug shader reads, and the Material its alpha calls for.
func (d debugDrawn) paint(e ecs.Entity, ref model.MeshRef, color m.Color, layers LayerMask) {
	d.meshes.UpdateFor(e, Mesh{Ref: ref, Layers: layers})
	d.params.UpdateFor(e, Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", color))})
	material := debugOpaqueMaterial
	if color.A < 1 {
		material = debugBlendedMaterial
	}
	d.materials.UpdateFor(e, material)
}

// debugKind is what the two Systems of one shape need to know about it,
// because a Component is plain data with no methods: how its geometry is
// built, and which of its fields are paint rather than geometry.
type debugKind[T debugShape] struct {
	// geometry appends the shape's vertices and indices, and appends none for
	// a shape with nothing to draw.
	geometry func(vertices []model.Vertex, indices []uint32, shape T) ([]model.Vertex, []uint32)
	// paint reports the shape's Color and Layers, and form the shape with
	// both cleared, so two shapes of equal form have equal geometry.
	paint func(shape T) (m.Color, LayerMask)
	form  func(shape T) T
}

// bakeSystem is a shape kind's bake System. It walks every shape with no Mesh
// - a new one, one whose geometry was degenerate until now, one whose Mesh
// was taken away - builds its geometry, bakes it and adds what draws it. A
// shape with nothing to draw is left without a Mesh and walked again next
// tick, which costs one geometry check.
//
// The Entities are collected before anything is added, so the walk never
// runs across its own insertions.
func (kind debugKind[T]) bakeSystem(
	k kernel.Kernel,
	shapes *ecs.Query[debugUnbaked[T]],
	meshes *ecs.Set[Mesh],
	params *ecs.Set[Params],
	materials *ecs.Set[Material],
	lookup *ecs.Write[*model.Lookup],
	work *ecs.Write[*debugScratch[T]],
) {
	s := work.Get()
	for e, shape := range shapes.All() {
		s.pending = append(s.pending, debugPending[T]{entity: e, shape: shape.Shape})
	}
	if len(s.pending) == 0 {
		return
	}
	la := model.NewLookupAccess(k, lookup.Get())
	drawn := debugDrawn{meshes: meshes, params: params, materials: materials}
	for _, p := range s.pending {
		// A shape whose Mesh was taken away still holds its bake.
		if old, ok := s.baked[p.entity]; ok {
			la.ReleaseMesh(old.ref)
			delete(s.baked, p.entity)
		}
		s.vertices, s.indices = kind.geometry(s.vertices[:0], s.indices[:0], p.shape)
		if len(s.indices) == 0 {
			continue
		}
		ref := la.BakeMesh(s.vertices, s.indices, gfx.TopologyTriangleList)
		if ref.ID() == 0 {
			continue
		}
		s.baked[p.entity] = debugBaked[T]{ref: ref, shape: p.shape}
		color, layers := kind.paint(p.shape)
		drawn.paint(p.entity, ref, color, layers)
	}
	clear(s.pending)
	s.pending = s.pending[:0]
}

// changeSystem is a shape kind's change System. For every baked shape a
// record names, it:
//
//   - releases the mesh when the shape was removed or its Entity despawned,
//     and on a living Entity takes away the Mesh, Params and Material it added;
//   - rebakes the mesh in place when a geometry field changed, which keeps the
//     ref and frees the buffers it replaces, or releases it as above when the
//     shape became one with nothing to draw;
//   - rewrites the Mesh's Layers, the Params and the Material when Color or
//     Layers changed.
//
// A shape that is not baked is the bake System's, so an addition is left to
// it; and a record's value is compared with what was baked rather than
// trusted to be new, so an addition that folded a later change in still
// reaches the mesh.
func (kind debugKind[T]) changeSystem(
	k kernel.Kernel,
	hooks *ecs.Hooks[T, ecs.HookAll],
	meshes *ecs.Set[Mesh],
	params *ecs.Set[Params],
	materials *ecs.Set[Material],
	dropMeshes *ecs.Remove[Mesh],
	dropParams *ecs.Remove[Params],
	dropMaterials *ecs.Remove[Material],
	lookup *ecs.Write[*model.Lookup],
	work *ecs.Write[*debugScratch[T]],
) {
	s := work.Get()
	if len(s.baked) == 0 {
		return
	}
	la := model.NewLookupAccess(k, lookup.Get())
	drawn := debugDrawn{meshes: meshes, params: params, materials: materials}
	release := func(e ecs.Entity, baked debugBaked[T], alive bool) {
		la.ReleaseMesh(baked.ref)
		delete(s.baked, e)
		if alive {
			dropMeshes.From(e)
			dropParams.From(e)
			dropMaterials.From(e)
		}
	}
	for e, hook := range hooks.All() {
		baked, ok := s.baked[e]
		if !ok {
			continue
		}
		if hook.IsRemoved() {
			release(e, baked, !hook.IsDespawned())
			continue
		}
		shape := hook.Value
		if shape == baked.shape {
			continue
		}
		if kind.form(shape) != kind.form(baked.shape) {
			s.vertices, s.indices = kind.geometry(s.vertices[:0], s.indices[:0], shape)
			if len(s.indices) == 0 || !la.UpdateMesh(baked.ref, s.vertices, s.indices) {
				release(e, baked, true)
				continue
			}
		}
		color, layers := kind.paint(shape)
		if was, wasLayers := kind.paint(baked.shape); color != was || layers != wasLayers {
			drawn.paint(e, baked.ref, color, layers)
		}
		s.baked[e] = debugBaked[T]{ref: baked.ref, shape: shape}
	}
}
