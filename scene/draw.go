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
	// material is the gfx material the draw's resolved tag entry named. It is
	// carried per draw rather than looked up again at emit time because
	// resolution is a pass-relative answer: the same scene material serves a
	// different gfx material in a shadow pass.
	material       *gfx.MaterialDescr
	materialOffset int
	firstInstance  int
	instances      int
}

// frameBuild is everything one flush accumulates before it emits: the three
// arenas each draw binds a range of, and the passes and draws waiting on them.
// Every slice in it keeps its backing across frames.
type frameBuild struct {
	instances arena
	frames    arena
	materials arena
	passes    []pendingPass
	draws     []pendingDraw
	// batches is the scratch one pass fills before publishing it, reused by
	// every pass in the frame.
	batches []BatchView
}

func (b *frameBuild) reset() {
	b.instances.reset()
	b.frames.reset()
	b.materials.reset()
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
	for i := range b.passes {
		pass := &b.passes[i]
		gfxWrite.Pass(pass.descr)
		for _, draw := range b.draws[pass.firstDraw : pass.firstDraw+pass.drawCount] {
			gfxWrite.DrawInstancedFrom(draw.mesh, *draw.material, draw.firstInstance, draw.instances,
				gfx.BufferRangeParam("sceneFrame", frames, pass.frameOffset, frameBlockSize),
				gfx.BufferRangeParam("sceneInstances", instances, pass.instanceOffset, pass.instanceBytes),
				gfx.BufferRangeParam("scenePbrMaterial", materials, draw.materialOffset, materialRecordSize),
			)
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

// addDraw packs one instance and its material record into the pass being
// accumulated. firstInstance is relative to the pass's own slice, which is what
// lets the draw read its instance with no offset plumbing of its own.
//
// One record per batch, no dedupe: two meshes sharing a material produce two
// byte-identical records, and collapsing them would cost a hash of every record
// every frame to save an upload nobody has measured.
func (b *frameBuild) addDraw(
	pass *pendingPass, mesh meshRecord, id uint32, entry materialEntry,
	world m.Mat4, record scenePbrRecord,
) {
	first := (len(b.instances.bytes()) - pass.instanceOffset) / instanceSize
	instance := packInstance(world)
	b.instances.appendElement(&instance)
	b.draws = append(b.draws, pendingDraw{
		mesh:           mesh.descr(),
		material:       entry.descr,
		materialOffset: b.materials.appendRecord(&record),
		firstInstance:  first,
		instances:      1,
	})
	b.batches = append(b.batches, BatchView{
		MeshID: id, MaterialID: entry.materialID,
		FirstInstance: first, InstanceCount: 1,
	})
}

// endPass closes the pass being accumulated.
func (b *frameBuild) endPass(pass *pendingPass) {
	pass.drawCount = len(b.draws) - pass.firstDraw
	pass.instanceBytes = len(b.instances.bytes()) - pass.instanceOffset
}
