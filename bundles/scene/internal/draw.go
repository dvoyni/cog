package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// pendingPass is one pass the recording System has decided but not yet
// emitted, and pendingDraw one draw inside it. Nothing is emitted while the
// arenas are still growing: a draw binds a range of an arena, and the arena
// has no buffer until it is complete.
type pendingPass struct {
	descr gfx.PassDescr
	// frameOffset locates this pass's sceneFrame block, and instanceOffset and
	// instanceBytes its slice of the frame's instances. Binding the slice
	// rather than the whole arena is what keeps instance_index pass-relative.
	frameOffset    int
	instanceOffset int
	instanceBytes  int
	firstDraw      int
	drawCount      int
}

type pendingDraw struct {
	mesh gfx.MeshDescr
	// skin is the group 2 buffers the draw binds, and its two flags are also
	// what picked the draw's shader variant: a half it does not have is a half
	// the module does not declare, so there is nothing left unbound.
	skin model.SkinBuffers
	// tag is the tag the Batch's material serves this pass with, and its set
	// the one the draw names. It is carried per draw because resolution is a
	// pass-relative answer: the same material serves a different set in a
	// shadow pass.
	tag           *materialTag
	firstInstance int
	instances     int
	// params are the Batch's Params, set before the ranges the recording
	// binds itself, so a Params value cannot displace them by naming one of
	// their names.
	params []gfx.ParameterDescr
}

// frameBuild is everything one frame accumulates before it emits: the arenas
// each draw binds a range of, and the passes and draws waiting on them. Every
// slice in it keeps its backing across frames.
type frameBuild struct {
	instances arena
	frames    arena
	// anims is the frame's sceneAnim arena. It is indexed absolutely rather
	// than bound per pass: an instance's AnimOffset counts vec4s from the
	// start of the whole buffer, so every draw binds it entire.
	anims arena
	// meshes is the frame's per-mesh record arena, bound entire like anims
	// because an instance's Mesh counts records from the start of the whole
	// buffer. Slot 0 is the reserved identity, written at every reset.
	meshes arena
	passes []pendingPass
	draws  []pendingDraw
	// opaque and blend are the two sort classes of the pass being built,
	// reused by every pass in the frame so the sort allocates nothing.
	opaque, blend []sortEntry
	// params is the scratch one draw's frame values are assembled in, and
	// values the material block a draw's Params members are laid over. gfx
	// copies both as it records, so one of each serves every draw in the
	// frame.
	params []gfx.ParameterDescr
	values model.PbrValues
}

func (b *frameBuild) reset() {
	b.instances.reset()
	b.frames.reset()
	b.anims.reset()
	b.meshes.reset()
	// Slot 0 first, before any draw can claim an index: the identity record is
	// what a custom-layout mesh and a UV-less standard mesh name.
	b.meshes.appendElement(&model.IdentityMesh)
	b.passes = b.passes[:0]
	b.draws = b.draws[:0]
}

// emit hands the frame to gfx: one upload per arena, then every pass with its
// draws behind it. Passes run in Order, not in emission order, so a pass may
// be declared here whenever its draws are known.
//
// A draw is its set's version for the frame, then the draw of the set: the
// Params first, then what the recording binds itself, each only where the
// set's shader declares it. SetDrawParams is last-wins per binding, so a
// Params value cannot displace one of scene's own ranges.
//
// The binding names are model's, because the shader that reads them is.
func (b *frameBuild) emit(k kernel.Kernel, gfxWrite *gfx.OpQueue) {
	if len(b.passes) == 0 {
		return
	}
	instances := gfxWrite.NewTemporaryBuffer(b.instances.bytes(), true)
	frames := gfxWrite.NewTemporaryBuffer(b.frames.bytes(), true)
	// sceneAnim is declared whether or not anything animates, so a frame that
	// packed no block still uploads one empty record: an unbound declared
	// binding is the silent whole-frame loss, not a degraded frame.
	anims := gfxWrite.NewTemporaryBuffer(b.animBytes(), true)
	meshes := gfxWrite.NewTemporaryBuffer(b.meshes.bytes(), true)
	for i := range b.passes {
		pass := &b.passes[i]
		ref := gfxWrite.NewPass(pass.descr)
		for j := range b.draws[pass.firstDraw : pass.firstDraw+pass.drawCount] {
			draw := &b.draws[pass.firstDraw+j]
			tag := draw.tag
			b.params = b.appendParams(b.params[:0], tag, draw.params)
			bound := tag.bindings
			if bound&bindFrame != 0 {
				b.params = append(b.params,
					gfx.BufferRangeParam(model.BindingSceneFrame, frames, pass.frameOffset, model.FrameBlockSize))
			}
			if bound&bindInstances != 0 {
				b.params = append(b.params,
					gfx.BufferRangeParam(model.BindingSceneInstances, instances, pass.instanceOffset, pass.instanceBytes))
			}
			if bound&bindAnim != 0 {
				b.params = append(b.params, gfx.BufferParam(model.BindingSceneAnim, anims))
			}
			if bound&bindMeshes != 0 {
				b.params = append(b.params, gfx.BufferParam(model.BindingSceneMeshes, meshes))
			}
			// Group 2 is bound only where the draw's variant declares it. The
			// two halves go separately because the variants split them: a
			// morph-only face declares binding 2 alone.
			if draw.skin.Bound {
				if bound&bindPoses != 0 {
					b.params = append(b.params, gfx.BufferParam(model.BindingScenePoses, draw.skin.Poses))
				}
				if bound&bindSkinJoints != 0 {
					b.params = append(b.params, gfx.BufferParam(model.BindingSceneSkinJoints, draw.skin.Joints))
				}
			}
			if draw.skin.Morphed && bound&bindMorphDeltas != 0 {
				b.params = append(b.params, gfx.BufferParam(model.BindingSceneMorphDeltas, draw.skin.Morphs))
			}
			gfxWrite.SetDrawParams(k, tag.set, b.params...)
			gfxWrite.DrawSet(ref, draw.mesh, tag.set, draw.instances, draw.firstInstance)
		}
	}
	clear(b.params)
}

// appendParams appends a Batch's Params as the frame's version sets them: a
// member of the material block laid over the tag's own values, which are then
// set whole, and any other binding by its name where the tag's shader declares
// it. gfx dropped a param no binding declared without a word, and one Params
// Component serves every tag of a material, so a binding one tag's shader
// lacks is not a mistake.
func (b *frameBuild) appendParams(dst []gfx.ParameterDescr, tag *materialTag, params []gfx.ParameterDescr) []gfx.ParameterDescr {
	if len(params) == 0 {
		return dst
	}
	members := false
	for i := range params {
		if !members && model.IsPbrValue(params[i].Name()) {
			b.values, members = tag.values, true
		}
		if b.values.Overlay(params[i]) {
			continue
		}
		if _, declared := tag.program.Binding(params[i].Name()); declared {
			dst = append(dst, params[i])
		}
	}
	if members && tag.bindings&bindPbrMaterial != 0 {
		// Borrowed rather than copied: the block is 160 bytes, past what a
		// param carries inline, and SetDrawParams copies it before it
		// returns.
		dst = append(dst, gfx.RawParameterRef(model.BindingScenePbrMaterial, &b.values))
	}
	return dst
}

// beginPass starts accumulating one pass, taking its sceneFrame block and the
// start of its instance slice.
func (b *frameBuild) beginPass(descr gfx.PassDescr, block model.FrameBlock) *pendingPass {
	b.passes = append(b.passes, pendingPass{
		descr:          descr,
		frameOffset:    b.frames.appendRecord(&block),
		instanceOffset: b.instances.beginRange(),
		firstDraw:      len(b.draws),
	})
	return &b.passes[len(b.passes)-1]
}

// addDraw packs one Batch's surviving instances in one pass as one instanced
// draw: one instance record per entry, packed contiguously through model's
// packer. firstInstance is relative to the pass's own slice, so the shader is
// the same whether the draw holds one instance or five thousand.
func (b *frameBuild) addDraw(
	pass *pendingPass, batch *batch, entry materialEntry,
	sorted []sortEntry, survivors []survivor, entries []entry,
) {
	first := (len(b.instances.bytes()) - pass.instanceOffset) / model.InstanceSize
	meshIndex := b.meshIndex(batch.mesh.UV)
	for _, sorted := range sorted {
		e := &entries[survivors[sorted.draw].draw]
		instance := model.PackInstance(e.world, e.anim, meshIndex)
		b.instances.appendElement(&instance)
	}
	b.draws = append(b.draws, pendingDraw{
		mesh:          batch.mesh.Descr(),
		skin:          batch.skin,
		tag:           entry.tag,
		firstInstance: first,
		instances:     len(sorted),
		params:        batch.params,
	})
}

// meshIndex is the slot a draw's instances name in the per-mesh record buffer.
// A mesh with no range of its own names slot 0, the reserved identity, and
// appends nothing.
func (b *frameBuild) meshIndex(record model.SceneMesh) uint32 {
	if record == (model.SceneMesh{}) {
		return 0
	}
	return uint32(b.meshes.appendElement(&record) / model.SceneMeshSize)
}

// endPass closes the pass being accumulated.
func (b *frameBuild) endPass(pass *pendingPass) {
	pass.drawCount = len(b.draws) - pass.firstDraw
	pass.instanceBytes = len(b.instances.bytes()) - pass.instanceOffset
}

// appendAnim appends one Entity's sceneAnim block to the frame's arena through
// model's packer and returns the AnimOffset its instances carry.
func (b *frameBuild) appendAnim(plays []model.ScenePlayRecord, morph model.AnimMorph) uint32 {
	var offset uint32
	b.anims.data, offset = model.AppendAnim(b.anims.data, plays, morph)
	return offset
}

// animBytes is the sceneAnim arena's upload. An empty arena still uploads one
// vec4, because NewTemporaryBuffer returns no buffer at all for no bytes and the
// binding is declared on every draw.
func (b *frameBuild) animBytes() []byte {
	if len(b.anims.bytes()) == 0 {
		return emptyAnimBlock[:]
	}
	return b.anims.bytes()
}

// emptyAnimBlock is one zeroed vec4, and it is read-only by convention:
// NewTemporaryBuffer copies what it is handed.
var emptyAnimBlock [16]byte
