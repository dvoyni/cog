package scene

import "github.com/dvoyni/cog/bundles/scene/internal"

// ModelDraw is everything one Model call says beyond which file it draws.
//
// A zero ModelDraw is a valid draw of the file's default scene at the origin,
// culled by the bounds the file declares.
type ModelDraw = internal.ModelDraw

// ModelLight is one KHR_lights_punctual light a model file declares, in the
// model's own space, with its node's flattened transform already applied.
//
// Lights are exposed as data and nothing converts one automatically. A file's
// lights are authored for the file, not for the scene it is dropped into: a
// lamp prop placed forty times would silently blow the sixteen-light per-pass
// cap, and which of a level's lights matter is the app's judgement, not the
// loader's. So an app reads these and declares the ones it wants through
// PointLight and SpotLight, at whatever world transform it drew the model at.
type ModelLight = internal.ModelLight

// ModelRef names what a scene- or node-scoped query is asking about, mirroring
// ModelDraw's own selectors field for field.
//
// It is a struct rather than three bare strings because the bare form has a
// transposition bug that compiles: Bounds(path, "crate", "") and
// Bounds(path, "", "crate") are both valid calls and mean different things.
//
// Only Nodes, Bounds and AABB take one. Everything else on the facade is per
// path, because path is the whole cache key: a model has one joint index space,
// and MorphTargets is one flattened list that Node re-rooting does not renumber.
type ModelRef = internal.ModelRef

// ModelState is one path's residency. It is a state rather than an absence
// precisely so that an in-flight load is distinguishable from a path nobody has
// asked for: a model drawn every frame while it loads must enqueue exactly one
// command.
type ModelState = internal.ModelState

const (
	// ModelMissing is a path the table has no entry for at all. It is the zero
	// value so that a map miss reads as missing without a second test.
	ModelMissing  = internal.ModelMissing
	ModelLoading  = internal.ModelLoading
	ModelResident = internal.ModelResident
	// ModelFailed is terminal. It never retries, and it clears only on unload -
	// a typo'd path must not spawn a load command every frame forever.
	ModelFailed = internal.ModelFailed
)

// ClipPlay is one animation clip playing on one model draw.
//
// Animation is stateless: nothing in scene advances Time, and no play survives
// the frame that recorded it. Gameplay - or the anim plugin - owns the clock
// and hands the result to the draw, which is what makes scrubbing, reversing
// and pausing the caller's business rather than an API scene has to grow.
//
// Clips are addressed by name, first match. An unknown name is reported once
// per model and the play dropped, so a typo costs the one play rather than the
// whole character.
type ClipPlay = internal.ClipPlay

// ClipInfo is one clip a model file declares, as Clips reports it.
//
// Duration is here because a caller needs it to know when a one-shot play has
// ended, which is a question only the clip's own length answers and the one
// piece of clip state gameplay cannot compute for itself.
type ClipInfo = internal.ClipInfo
