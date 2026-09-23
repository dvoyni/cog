package types

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// ModelDraw is everything one Model call says beyond which file it draws.
//
// A zero ModelDraw is a valid draw of the file's default scene at the origin,
// culled by the bounds the file declares.
type ModelDraw struct {
	// Transform places the single instance. Transforms, when it is non-empty,
	// overrides it and places one instance per entry, exactly as MeshDraw does.
	//
	// Instancing is per primitive: a six-primitive model drawn at a hundred
	// transforms is six batches of a hundred, not six hundred draw calls. The
	// instances share the draw's animation, so a hundred crates is one call and
	// a hundred independently-animated characters is a hundred.
	Transform  m.Transform
	Transforms []m.Transform

	// Scene names an entry in the file's scenes array; empty is the file's
	// declared default. glTF scene names are optional, so a file whose scenes
	// are unnamed has no addressable scene but that default.
	//
	// Node names a node within that scene, and empty is the whole scene. It is
	// a plain name matched against the first depth-first node carrying it, not
	// a slash path: a path would make the selector a parsed string, and glTF
	// node names are not unique enough to make one mean anything.
	//
	// A Node draw re-roots. The node's authored world transform inside the file
	// is discarded and Transform replaces it, descendants keeping their
	// relative transforms, so props.glb with Node "crate" behaves as an
	// independent asset however the artist laid the file out. An empty Node
	// keeps the scene's root transforms, because a scene is authored as one
	// unit.
	//
	// Neither selector falls back. A Scene or Node that matches nothing skips
	// the draw and reports once: one typo'd node name rendering an entire
	// building at the origin is the worse failure.
	Scene string
	Node  string

	// Plays are the animation clips this draw blends, up to four. An empty
	// Plays draws the model's rest pose, which is a real pose rather than a
	// collapse: row 0 of every model is the authored hierarchy resolved once.
	//
	// Weights are normalised across the plays before anything is packed, so
	// they express proportions rather than intensities: two plays at 1.0 each
	// is an even blend, and so is two at 0.1. A total of about zero falls back
	// to the rest pose.
	//
	// The slice is copied into the frame's own arena, so a caller may reuse
	// its backing the moment the call returns. A draw's instances share its
	// plays - a hundred crates is one call, and a hundred independently
	// animated characters is a hundred calls.
	Plays []model.ClipPlay

	// MorphWeights are this draw's morph target weights, positional over the
	// model's whole flattened target list - which MorphTargets(path) names, in
	// depth-first node order, one entry per target of every morphed node.
	//
	// It is the one index-addressed thing in the plugin. Name addressing would
	// put about fifty-two map hits per face per frame on the recording path to
	// re-derive a mapping the caller computed at startup; naming lives on the
	// lookup facade instead, so this path is a memcpy.
	//
	// A non-nil value overrides the animated result wholesale; nil falls back
	// to the animated weights, then node.weights, then mesh.weights, then zero.
	// A short slice leaves the remaining targets at 0 and a long one ignores
	// the tail and reports once per model. Neither is an error.
	//
	// The slice is copied into the frame's own arena, so a caller may reuse its
	// backing the moment the call returns. A draw's instances share it, exactly
	// as they share Plays.
	MorphWeights []float32

	// Material replaces the file's own materials wholesale, and nil is the
	// file's. A non-nil value is bound instead of every material the load
	// built, and the file's params are not bound at all - its textures, base
	// colours, factors and texture transforms do not survive. That is the dissolve, the
	// silhouette and the depth-only case, where binding the artist's numbers
	// under a shader that never heard of them would be a wrong picture with
	// nothing in the frame to explain it.
	//
	// It is copied into the frame's own arenas at record, exactly as a
	// MeshDraw's Material is, so a caller may reuse or change it the moment
	// the call returns.
	//
	// It does not overlap with OverrideParams. This one replaces and that one
	// merges, and a draw may still use both: the replacement takes glTF's own
	// defaults for its record and the overrides merge over those.
	Material Material

	// OverrideParams merges by name over each primitive's own material,
	// keeping the file's textures. That is the team-colour, hit-flash and fade
	// case. glTF's parameter names are the user-facing contract, so
	// gfx.ColorParam("baseColorFactor", c) is what tints a model, and the glTF
	// specification is the documentation of what each name means.
	//
	// It broadcasts to every material the draw binds - all six of a
	// six-material model's - which is what the common per-draw override
	// actually wants. Matching is against the resolved tag entry, and a name
	// that entry's shader does not declare is ignored rather than reported:
	// that is what keeps the broadcast safe across tags, since an alphaMode
	// MASK shadow shader declares baseColorTexture and alphaCutoff where an
	// OPAQUE one declares neither.
	//
	// A nil Material with no overrides binds the file's records directly, with
	// no copy of either.
	//
	// The slice is copied into the frame's own arena, so a caller may reuse its
	// backing the moment the call returns, exactly as with Plays.
	OverrideParams []gfx.ParameterDescr
}

// ModelDrawRecord is one recorded Model call. It is kept apart from DrawRecord
// because a model draw expands into one draw per primitive at flush time, and
// the expansion needs the path to be resolved against residency first - a
// unloaded model contributes no draws at all.
type ModelDrawRecord struct {
	Layers LayerMask
	Path   string
	// scene and node are the draw's selectors, resolved against the loaded
	// model at expansion rather than at record: the file is not read until the
	// flush, and a selector means nothing until it is.
	Scene, Node string
	transform   m.Transform
	// transforms aliases the recording's transform arena, never the caller's
	// array, and is empty for a single-instance draw.
	transforms []m.Transform
	// Plays aliases the recording's play arena for the same reason, and
	// morphWeights the recording's weight arena. A nil morphWeights is the
	// draw taking the animated result; an empty non-nil one is the caller
	// asking for every target at zero, which are different answers.
	Plays        []model.ClipPlay
	MorphWeights []float32
	Overridden   bool
	// Material is the caller's replacement for the file's own, nil when the
	// draw takes the file's. It and overrides alias the recording's material
	// and parameter arenas for the same reason plays and morphWeights alias
	// theirs.
	Material  Material
	Overrides []gfx.ParameterDescr
	// MaterialKey is Material's content key, taken at record; zero when
	// Material is nil or empty.
	MaterialKey MaterialKey
}

// Model records one draw of the glTF file at path.
//
// Loading is synchronous and a model that could not be loaded is skipped, never
// substituted: the flush this call is recorded into reads, parses and uploads
// the file, so the model draws in this same frame - and a large file hitches
// it. Preload is the lever that moves that cost somewhere the game chose. A
// path that failed is never loaded again - a typo must not re-read the file
// every frame forever - and clears only on unload, where there is no
// placeholder either way.
//
// Failures report once through kernel.ReportError, from the flush that hit
// them: the read failure from the asset library, under the descriptor, and
// everything the decode finds wrong from scene, under the path.
func (q *OpQueue) Model(layers LayerMask, path string, draw ModelDraw) {
	transforms := draw.Transforms
	if len(transforms) > 0 {
		start := len(q.meshes.transforms)
		q.meshes.transforms = append(q.meshes.transforms, transforms...)
		transforms = q.meshes.transforms[start:len(q.meshes.transforms):len(q.meshes.transforms)]
	}
	plays := draw.Plays
	if len(plays) > 0 {
		start := len(q.plays)
		q.plays = append(q.plays, plays...)
		plays = q.plays[start:len(q.plays):len(q.plays)]
	}
	weights := draw.MorphWeights
	overridden := weights != nil
	if len(weights) > 0 {
		start := len(q.morphWeights)
		q.morphWeights = append(q.morphWeights, weights...)
		weights = q.morphWeights[start:len(q.morphWeights):len(q.morphWeights)]
	}
	overrides := draw.OverrideParams
	if len(overrides) > 0 {
		start := len(q.meshes.params)
		q.meshes.params = append(q.meshes.params, overrides...)
		overrides = q.meshes.params[start:len(q.meshes.params):len(q.meshes.params)]
	}
	var key MaterialKey
	draw.Material, key = q.meshes.copyMaterial(draw.Material)
	draw.Transforms, draw.Plays, draw.MorphWeights = transforms, plays, weights
	draw.OverrideParams = overrides
	q.calls = append(q.calls, Op{Kind: OpModel, Layers: layers, Path: path, Model: draw})
	q.models = append(q.models, ModelDrawRecord{
		Layers: layers, Path: path, Scene: draw.Scene, Node: draw.Node,
		transform: draw.Transform, transforms: transforms, Plays: plays,
		MorphWeights: weights, Overridden: overridden,
		Material: draw.Material, Overrides: overrides, MaterialKey: key,
	})
}

// flushModels lists the model draws the flush is consuming, in recording order.
func (q *OpQueue) flushModels() []ModelDrawRecord { return q.publishedModels }

// Instances resolves one model draw's placements, reading a single-instance
// draw as the one-element case so the expansion has one shape.
func (r ModelDrawRecord) Instances(single *[1]m.Transform) []m.Transform {
	if len(r.transforms) > 0 {
		return r.transforms
	}
	single[0] = r.transform
	return single[:]
}
