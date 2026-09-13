package sceneimpl

import "github.com/dvoyni/cog/bundles/scene/internal"

// packAnim writes one draw's sceneAnim block and returns the animOffset an
// instance carries, or SceneNoAnim when the draw animates nothing.
//
// The block is indirect because sceneInstances is bound once per pass and
// shared by every draw in it: a fixed per-instance record would have to be
// sized for the worst case and put a ~320 byte tax on every debug line against
// about 48 bytes of content.
//
// A skinned draw packs one block per call and a morphed one packs a block per
// primitive, because the three morph words are per-primitive constants. Putting
// them in the per-batch material record would remove that duplication exactly,
// and was rejected: it would put scene geometry constants into a record gfx
// packs on the render thread while scene records on the update thread.
func (b *frameBuild) packAnim(plays []internal.ScenePlayRecord, morph morphBlock) uint32 {
	if len(plays) == 0 && len(morph.targets) == 0 {
		return internal.SceneNoAnim
	}
	offset := len(b.anims.bytes()) / 16
	header := internal.SceneAnimHeader{
		PlayCount:   uint32(len(plays)),
		TargetCount: uint32(len(morph.targets)),
		MorphBase:   morph.binding.Base,
		MorphStride: morph.binding.Stride,
	}
	b.anims.appendElement(&header)
	for i := range plays {
		b.anims.appendElement(&plays[i])
	}
	for i := range morph.targets {
		b.anims.appendElement(&morph.targets[i])
	}
	// Two morph entries share one vec4, so an odd count leaves the arena half
	// a vec4 short. animOffset counts vec4s, so the next block would then start
	// at an offset no instance can name.
	b.anims.padToVec4()
	return uint32(offset)
}

// morphBlock is the morph half of one draw's sceneAnim block: the primitive's
// addressing constants and the sparse list of targets that survived the cull.
type morphBlock struct {
	binding internal.MorphBinding
	targets []internal.SceneMorphWeight
}
