package ecsscene

import (
	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// The four Queries the recording System walks. Every field is a read — a value
// field yields a copy — because recording changes nothing about an Entity, and
// each names Transform because an Entity with nowhere to stand is not recorded.
//
// Animation, Params and Material are deliberately not fields. A Query matches
// an Entity having at least the Components it names, so naming an optional one
// would drop every Entity without it out of the walk; they are reached through
// accessors instead, at one probe each.
type (
	modelQuery struct {
		Place Transform
		Model Model
	}
	meshQuery struct {
		Place Transform
		Mesh  Mesh
	}
	lightQuery struct {
		Place Transform
		Light Light
	}
	cameraQuery struct {
		Place  Transform
		Camera Camera
	}
)

// scratch is where the recording System builds the slices scene's calls take.
// It is a resource the plugin owns, not something the System's closure
// captured: anything a System keeps between calls is in its lock set, so the
// kernel, not a comment, is what keeps two holders apart.
//
// Every slice is grown once and reused. A slice is reset per draw — scene
// copies what a call is given into its frame arenas before the call returns —
// with one exception inside a draw: tagParams is appended to per material tag
// and never reset between tags, because gfx keeps the params slice a
// descriptor is built around, and one tag must not overwrite another's before
// scene has copied them.
type scratch struct {
	plays     []scene.ClipPlay
	params    []gfx.ParameterDescr
	tags      scene.Material
	tagParams []gfx.ParameterDescr
	passes    []scene.Pass
}

// newScratch allocates the backings whose bound is known. tags starts non-nil
// so that a present Material with no tags reaches scene as an empty material
// rather than as nil, which scene reads as no material at all.
func newScratch() *scratch {
	return &scratch{
		plays: make([]scene.ClipPlay, 0, MaxPlays),
		tags:  make(scene.Material, 0, 4),
	}
}

// record is the binding: every Entity with a Transform and a Model, Mesh,
// Light or Camera, copied into scene's op queue once a tick.
//
// Its signature is its whole lock set — the Stores it reads, the scratch and
// scene's queue it writes — and it declares no ordering: scene's flush is
// subscribed Last, so an ordinary-phase System already runs before it, and a
// game System that moves Transforms orders itself Before UpdateEventHandler.
func record(
	models *ecs.Query[modelQuery],
	meshes *ecs.Query[meshQuery],
	lights *ecs.Query[lightQuery],
	cameras *ecs.Query[cameraQuery],
	animations *ecs.Get[Animation],
	params *ecs.Get[Params],
	materials *ecs.Get[Material],
	work *ecs.Write[*scratch],
	out *ecs.Write[*scene.OpQueue],
) {
	// Both handles are read once, outside the loops: neither value is a place
	// to keep anything past the body of this call.
	s, queue := work.Get(), out.Get()
	for e, it := range models.All() {
		draw := scene.ModelDraw{
			Transform: scene.Transform(it.Place),
			Scene:     it.Model.Ref.Scene,
			Node:      it.Model.Ref.Node,
		}
		if animation, ok := animations.Of(e); ok {
			draw.Plays = s.clipPlays(&animation)
		}
		if p, ok := params.Of(e); ok {
			draw.OverrideParams = s.drawParams(&p)
		}
		if material, ok := materials.Of(e); ok {
			draw.Material = s.material(&material)
		}
		queue.Model(it.Model.Layers, it.Model.Ref.Path, draw)
	}
	for e, it := range meshes.All() {
		draw := scene.MeshDraw{
			Transform: scene.Transform(it.Place),
			Bounds:    it.Mesh.Bounds,
			NeverCull: it.Mesh.NeverCull,
		}
		if p, ok := params.Of(e); ok {
			draw.Params = s.drawParams(&p)
		}
		if material, ok := materials.Of(e); ok {
			draw.Material = s.material(&material)
		}
		queue.Mesh(it.Mesh.Layers, it.Mesh.Ref, draw)
	}
	for _, it := range lights.All() {
		light := scene.LightDescr{
			Position:  it.Place.Position,
			Color:     it.Light.Color,
			Intensity: it.Light.Intensity,
			Range:     it.Light.Range,
			InnerCone: it.Light.InnerCone,
			OuterCone: it.Light.OuterCone,
		}
		if it.Light.Kind == scene.LightSpot {
			// m.Quat.Rotate reads the zero Quat as no rotation, exactly as
			// scene reads a Transform's, so an unrotated spot shines down -Z.
			light.Direction = it.Place.Rotation.Rotate(facing)
			queue.SpotLight(it.Light.Layers, light)
			continue
		}
		queue.PointLight(it.Light.Layers, light)
	}
	for _, it := range cameras.All() {
		queue.Camera(it.Camera.ID, scene.CameraDescr{
			Transform:        scene.Transform(it.Place),
			Projection:       it.Camera.Projection,
			FovY:             it.Camera.FovY,
			Height:           it.Camera.Height,
			Shear:            it.Camera.Shear,
			Near:             it.Camera.Near,
			Far:              it.Camera.Far,
			CullMask:         it.Camera.CullMask,
			SunDirection:     it.Camera.SunDirection,
			SunColor:         it.Camera.SunColor,
			SunIntensity:     it.Camera.SunIntensity,
			AmbientSky:       it.Camera.AmbientSky,
			AmbientGround:    it.Camera.AmbientGround,
			AmbientIntensity: it.Camera.AmbientIntensity,
			Passes:           s.cameraPasses(&it.Camera),
		})
	}
	s.release()
}

// facing is the direction an unrotated Transform faces, which is the way
// scene.LookAt aims.
var facing = m.Vec3{Z: -1}

// clipPlays flattens an Animation's used slots into scratch.
func (s *scratch) clipPlays(animation *Animation) []scene.ClipPlay {
	plays := s.plays[:0]
	for i := range animation.Plays {
		if animation.Plays[i].Clip != "" {
			plays = append(plays, animation.Plays[i])
		}
	}
	s.plays = plays
	return plays
}

// drawParams copies a Params List out through All, which is the only route
// out of a List and costs no allocation into a reused backing.
func (s *scratch) drawParams(params *Params) []gfx.ParameterDescr {
	out := s.params[:0]
	for _, param := range params.Values.All() {
		out = append(out, param)
	}
	s.params = out
	return out
}

// material rebuilds a Material Component as the scene.Material it describes.
// Each tag's params are a full-slice-expression window of tagParams, so no tag
// can append into the next one's, and a growth part-way through a draw leaves
// the earlier tags on the old backing, which nothing writes again.
func (s *scratch) material(material *Material) scene.Material {
	tags, params := s.tags[:0], s.tagParams[:0]
	for _, tag := range material.Tags.All() {
		start := len(params)
		for _, param := range tag.Params.All() {
			params = append(params, param)
		}
		own := params[start:len(params):len(params)]
		tags = append(tags, scene.MaterialTag{
			Tag:   tag.Tag,
			Descr: gfx.MaterialWithState(tag.Shader, tag.State, own...),
		})
	}
	s.tags, s.tagParams = tags, params
	return tags
}

// cameraPasses copies a Camera's passes out into scratch. An empty List is an
// empty slice, which scene reads as its one default pass.
func (s *scratch) cameraPasses(camera *Camera) []scene.Pass {
	passes := s.passes[:0]
	for _, pass := range camera.Passes.All() {
		passes = append(passes, pass)
	}
	s.passes = passes
	return passes
}

// release zeroes every backing to its capacity once the frame is recorded.
// What the scratch last held is a copy of some Entity's Components — names,
// and Blob bytes that may be a texture's pixels — and a backing that kept them
// would keep a despawned Entity's data reachable until a later draw happened
// to overwrite the same slots. The backings are a draw wide, so this is cheap.
func (s *scratch) release() {
	clear(s.plays[:cap(s.plays)])
	clear(s.params[:cap(s.params)])
	clear(s.tags[:cap(s.tags)])
	clear(s.tagParams[:cap(s.tagParams)])
	clear(s.passes[:cap(s.passes)])
}
