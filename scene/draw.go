package scene

import (
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// The sizes of the records scene binds ranges of. They are the Go structs'
// sizes because the Go structs are what scene writes; the WGSL side of the same
// contract is asserted where the shader is reflected.
var (
	instanceSize       = int(unsafe.Sizeof(sceneInstance{}))
	frameBlockSize     = int(unsafe.Sizeof(sceneFrameBlock{}))
	materialRecordSize = int(unsafe.Sizeof(scenePbrRecord{}))
)

// pendingPass is one pass the flush has decided but not yet emitted, and
// pendingDraw one draw inside it. Nothing is emitted while the arenas are still
// growing: a draw binds a range of an arena, and the arena has no buffer until
// it is complete.
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
	// skin is the pair of group 2 buffers the draw binds: the model's own
	// baked records, or the shared null skin for everything buffer-built. A
	// declared binding must be bound or the whole frame's command buffer
	// vanishes silently, so this is never zero.
	skin skinBuffers
	// material is the gfx material the draw's resolved tag entry named. It is
	// carried per draw rather than looked up again at emit time because
	// resolution is a pass-relative answer: the same scene material serves a
	// different gfx material in a shadow pass.
	material       *gfx.MaterialDescr
	materialOffset int
	firstInstance  int
	instances      int
	// params are the extra parameters the draw asked to bind, aliasing the
	// recording's arena. They are bound after the three ranges scene binds
	// itself, so a caller cannot displace them by naming one of their names.
	params []gfx.ParameterDescr
}

// frameBuild is everything one flush accumulates before it emits: the three
// arenas each draw binds a range of, and the passes and draws waiting on them.
// Every slice in it keeps its backing across frames.
type frameBuild struct {
	instances arena
	frames    arena
	materials arena
	// anims is the frame's sceneAnim arena. It is indexed absolutely rather
	// than bound per pass: an instance's animOffset counts vec4s from the
	// start of the whole buffer, so every draw binds it entire.
	anims  arena
	passes []pendingPass
	draws  []pendingDraw
	// batches is the scratch one pass fills before publishing it, reused by
	// every pass in the frame.
	batches []BatchView
	// opaque and blend are the two sort classes of the pass being built, reused
	// by every pass in the frame so the sort allocates nothing.
	opaque, blend []sortEntry
	// params is the scratch one draw's full parameter list is assembled in.
	// gfx copies parameters into its own arena as it records, so one slice
	// serves every draw in the frame.
	params []gfx.ParameterDescr
	// worlds is the scratch one batch's instance matrices are gathered into
	// before they are packed, reused by every batch in the frame.
	worlds []m.Mat4
}

func (b *frameBuild) reset() {
	b.instances.reset()
	b.frames.reset()
	b.materials.reset()
	b.anims.reset()
	b.passes = b.passes[:0]
	b.draws = b.draws[:0]
	b.batches = b.batches[:0]
}

// emit hands the frame to gfx: one upload per arena, then every pass with its
// draws behind it. Passes run in Order, not in emission order, so a pass may be
// declared here whenever its draws are known.
func (b *frameBuild) emit(gfxWrite *gfx.OpQueue) {
	instances := gfxWrite.TemporaryBuffer(b.instances.bytes(), true)
	frames := gfxWrite.TemporaryBuffer(b.frames.bytes(), true)
	materials := gfxWrite.TemporaryBuffer(b.materials.bytes(), true)
	// sceneAnim is declared whether or not anything animates, so a frame that
	// packed no block still uploads one empty record: an unbound declared
	// binding is the silent whole-frame loss, not a degraded frame.
	anims := gfxWrite.TemporaryBuffer(b.animBytes(), true)
	for i := range b.passes {
		pass := &b.passes[i]
		gfxWrite.Pass(pass.descr)
		for _, draw := range b.draws[pass.firstDraw : pass.firstDraw+pass.drawCount] {
			b.params = append(b.params[:0],
				gfx.BufferRangeParam("sceneFrame", frames, pass.frameOffset, frameBlockSize),
				gfx.BufferRangeParam("sceneInstances", instances, pass.instanceOffset, pass.instanceBytes),
				gfx.BufferParam("sceneAnim", anims),
				gfx.BufferRangeParam("scenePbrMaterial", materials, draw.materialOffset, materialRecordSize),
				gfx.BufferParam("scenePoses", draw.skin.poses),
				gfx.BufferParam("sceneSkinJoints", draw.skin.joints),
			)
			b.params = append(b.params, draw.params...)
			gfxWrite.DrawInstancedFrom(draw.mesh, *draw.material,
				draw.firstInstance, draw.instances, b.params...)
		}
	}
}

// beginPass starts accumulating one pass, taking its sceneFrame block and the
// start of its instance slice.
func (b *frameBuild) beginPass(descr gfx.PassDescr, block sceneFrameBlock) *pendingPass {
	b.passes = append(b.passes, pendingPass{
		descr:          descr,
		frameOffset:    b.frames.appendRecord(&block),
		instanceOffset: b.instances.beginRange(),
		firstDraw:      len(b.draws),
	})
	b.batches = b.batches[:0]
	return &b.passes[len(b.passes)-1]
}

// addDraw packs one batch into the pass being accumulated: one instance per
// world matrix, packed contiguously, one material record, and one entry in the
// pass's batch list. firstInstance is relative to the pass's own slice, which
// is what lets the batch read its instances with no offset plumbing of its own
// - WebGPU's instance_index starts at firstInstance, so the shader is the same
// whether the batch holds one instance or a thousand.
//
// One record per batch, no dedupe: two meshes sharing a material produce two
// byte-identical records, and collapsing them would cost a hash of every record
// every frame to save an upload nobody has measured. A batch is one instanced
// call's survivors, so while the automatic collapse of consecutive equal draws
// is deferred the table degenerates to one record per draw for everything else.
func (b *frameBuild) addDraw(
	pass *pendingPass, mesh meshRecord, id uint32, entry materialEntry,
	worlds []m.Mat4, record scenePbrRecord, params []gfx.ParameterDescr, anim animBinding,
) {
	first := (len(b.instances.bytes()) - pass.instanceOffset) / instanceSize
	for _, world := range worlds {
		instance := packInstance(world, anim)
		b.instances.appendElement(&instance)
	}
	b.draws = append(b.draws, pendingDraw{
		mesh:           mesh.descr(),
		skin:           anim.skin,
		material:       entry.descr,
		materialOffset: b.materials.appendRecord(&record),
		firstInstance:  first,
		instances:      len(worlds),
		params:         params,
	})
	b.batches = append(b.batches, BatchView{
		MeshID: id, MaterialID: entry.materialID,
		FirstInstance: first, InstanceCount: len(worlds),
	})
}

// endPass closes the pass being accumulated.
func (b *frameBuild) endPass(pass *pendingPass) {
	pass.drawCount = len(b.draws) - pass.firstDraw
	pass.instanceBytes = len(b.instances.bytes()) - pass.instanceOffset
}

// animBytes is the sceneAnim arena's upload. An empty arena still uploads one
// vec4: TemporaryBuffer returns no buffer at all for no bytes, and the binding
// is declared on every draw whether or not the frame animated anything - so an
// empty one would be the unbound binding that takes the frame down silently.
//
// The placeholder is a package-level array rather than a fresh allocation,
// because a frame that animates nothing is the common frame and the arenas'
// whole discipline is that a steady frame allocates nothing.
func (b *frameBuild) animBytes() []byte {
	if len(b.anims.bytes()) == 0 {
		return emptyAnimBlock[:]
	}
	return b.anims.bytes()
}

// emptyAnimBlock is one zeroed vec4, and it is read-only by convention:
// TemporaryBuffer copies what it is handed.
var emptyAnimBlock [16]byte
