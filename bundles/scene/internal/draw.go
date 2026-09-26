package internal

import (
	"github.com/dvoyni/cog/bundles/model"
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
	// material is the gfx material the Batch's material serves this pass
	// with. It is carried per draw because resolution is a pass-relative
	// answer: the same material serves a different gfx material in a shadow
	// pass.
	material      *gfx.MaterialDescr
	firstInstance int
	instances     int
	// params are the Batch's Params, bound after the ranges the recording
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
	// params is the scratch one draw's full parameter list is assembled in.
	// gfx copies parameters into its own arena as it records, so one slice
	// serves every draw in the frame.
	params []gfx.ParameterDescr
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
// The binding names are model's, because the shader that reads them is.
func (b *frameBuild) emit(gfxWrite *gfx.OpQueue) {
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
		for _, draw := range b.draws[pass.firstDraw : pass.firstDraw+pass.drawCount] {
			b.params = append(b.params[:0],
				gfx.BufferRangeParam(model.BindingSceneFrame, frames, pass.frameOffset, model.FrameBlockSize),
				gfx.BufferRangeParam(model.BindingSceneInstances, instances, pass.instanceOffset, pass.instanceBytes),
				gfx.BufferParam(model.BindingSceneAnim, anims),
				gfx.BufferParam(model.BindingSceneMeshes, meshes),
			)
			// Group 2 is bound only where the draw's variant declares it. The
			// two halves go separately because the variants split them: a
			// morph-only face declares binding 2 alone.
			if draw.skin.Bound {
				b.params = append(b.params,
					gfx.BufferParam(model.BindingScenePoses, draw.skin.Poses),
					gfx.BufferParam(model.BindingSceneSkinJoints, draw.skin.Joints))
			}
			if draw.skin.Morphed {
				b.params = append(b.params, gfx.BufferParam(model.BindingSceneMorphDeltas, draw.skin.Morphs))
			}
			b.params = append(b.params, draw.params...)
			gfxWrite.Draw(ref, draw.mesh, *draw.material,
				draw.instances, draw.firstInstance, b.params...)
		}
	}
	clear(b.params)
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
	pass *pendingPass, batch *batch, material materialEntry,
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
		material:      material.descr,
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
