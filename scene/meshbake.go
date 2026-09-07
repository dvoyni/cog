package scene

import (
	"github.com/dvoyni/cog/gfx"
)

// span locates one stretch of the staging arena. Offsets rather than a slice
// because the arena grows between the call that staged the bytes and the flush
// that uploads them, and growing may move it.
type span struct{ at, size int }

func (s span) of(arena []byte) []byte {
	if s.size == 0 {
		return nil
	}
	return arena[s.at : s.at+s.size]
}

// pendingMesh is one deferred upload: the mesh table slot it lands in, the
// generation that slot held when it was queued, and the staged bytes. The
// generation is what makes a mesh released before the flush drained its bake
// drop the upload instead of writing into whatever now holds the slot.
type pendingMesh struct {
	id         uint32
	generation uint32
	vertices   span
	indices    span
	rebake     bool
}

// meshBaker is the flush's GPU half, handed to the Lookup for the length of one
// drain. The Lookup takes three functions rather than the resource queue itself
// because LookupAccess is deliberately GPU-free: BakeBuffer dereferences its
// backend with no nil guard, so a mesh baked at startup through a queue held in
// an app handler would either panic or silently not exist.
type meshBaker struct {
	bake    func(data []byte) gfx.BufferDescr
	rebake  func(buffer gfx.BufferDescr, data []byte) gfx.BufferDescr
	release func(buffer gfx.BufferDescr)
}

// BakeMesh registers caller-owned geometry and returns the ref that draws it.
//
// The ref is minted and returned immediately; the upload is queued onto the
// Lookup for scene's own flush to drain, so a mesh baked and drawn in the same
// update handler still uploads in that frame. The vertices are copied into
// scene's staging arena here, and handed to gfx without a second copy at the
// flush.
//
// TVertex must be a pointer-free plain-data struct whose VertexLayout matches
// its memory layout. Anything but scene.Vertex is a custom layout, and a custom
// layout requires a custom Material: the bundled PBR is one shader module with
// one vertex stage and no entry-point selection, so a draw that pairs a custom
// layout with the bundled material is reported and skipped.
//
// Invalid geometry - no vertices, an index past the last vertex, or an index
// count that is not a multiple of three under TriangleList - is reported and
// yields a zero MeshRef, which draws nothing.
func (la LookupAccess) BakeMesh[TVertex VertexLayout](
	vertices []TVertex, indices []uint32, topology gfx.PrimitiveTopology,
) MeshRef {
	if !la.Valid() {
		return MeshRef{}
	}
	input, err := mintMesh[TVertex](&la.lookup.layouts, vertices, indices, topology, true)
	if err != nil {
		la.kernel.ReportError(err)
		return MeshRef{}
	}
	ref := la.lookup.claimMesh(meshRecord{
		vertexCount: input.vertexCount, indexCount: input.indexCount,
		topology: input.topology, layout: input.layout, layoutID: input.layoutID,
		standard: input.standard, bounds: input.bounds,
	})
	la.lookup.stage(ref, input, false)
	return ref
}

// UpdateMesh replaces a durable mesh's geometry wholesale, at any size, keeping
// its ref and its id. It reports and refuses a change of vertex layout or
// topology, both of which the pipeline key and the sort assume are fixed for a
// ref's life, a call on a temporary or released ref, and geometry that would
// have been invalid at bake time. It reports whether the update was accepted.
//
// There is no capacity concept: gfx re-bakes a buffer at any length while
// preserving its id, so growth is free. Sizing to a maximum vertex count is a
// performance note, not a constraint this API encodes.
//
// The bounding sphere is recomputed from the new vertices, so a mesh that grows
// past its old bounds still culls correctly.
func (la LookupAccess) UpdateMesh[TVertex VertexLayout](
	ref MeshRef, vertices []TVertex, indices []uint32,
) bool {
	if !la.Valid() {
		return false
	}
	if ref.source == meshTemporary {
		la.kernel.ReportError(ErrMeshUpdateRejected{
			Mesh: ref.ID(), Reason: "it is a temporary mesh, which is rebuilt by recording it again",
		})
		return false
	}
	record, ok := la.lookup.mesh(ref)
	if !ok {
		la.kernel.ReportError(ErrMeshUnavailable{Mesh: ref.ID()})
		return false
	}
	input, err := mintMesh[TVertex](&la.lookup.layouts, vertices, indices, record.topology, true)
	if err != nil {
		la.kernel.ReportError(err)
		return false
	}
	if input.layoutID != record.layoutID {
		la.kernel.ReportError(ErrMeshUpdateRejected{
			Mesh: ref.ID(), Reason: "it changes the vertex layout, which is fixed for the ref's life",
		})
		return false
	}
	record.vertexCount, record.indexCount = input.vertexCount, input.indexCount
	record.bounds = input.bounds
	la.lookup.meshes[ref.id-1] = record
	la.lookup.stage(ref, input, true)
	return true
}

// ReleaseMesh gives up a durable mesh. The ref goes stale at once, so anything
// that draws it afterwards is reported and skipped rather than drawing whatever
// later takes the slot; the buffers themselves are freed at the frame boundary.
func (la LookupAccess) ReleaseMesh(ref MeshRef) {
	if !la.Valid() {
		return
	}
	if !la.lookup.releaseMesh(ref) {
		la.kernel.ReportError(ErrMeshUnavailable{Mesh: ref.ID()})
	}
}

// releaseMesh retires one mesh slot and queues its buffers, and reports whether
// the ref named a live mesh. It is shared with the model unload, which frees a
// resident model's geometry through exactly this path - the alternative being a
// second retirement rule that could drift from this one.
func (l *Lookup) releaseMesh(ref MeshRef) bool {
	record, ok := l.mesh(ref)
	if !ok {
		return false
	}
	if record.baked {
		l.pendingReleases = append(l.pendingReleases, record.vertices)
		if record.indexed {
			l.pendingReleases = append(l.pendingReleases, record.indices)
		}
	}
	// The slot keeps only the bumped generation, which is what a later ref to
	// the mesh that used to live here fails against.
	l.meshes[ref.id-1] = meshRecord{generation: ref.generation + 1}
	l.freeMeshes = append(l.freeMeshes, ref.id)
	return true
}

// stage copies one mint's bytes into the staging arena and queues the upload.
func (l *Lookup) stage(ref MeshRef, input meshInput, rebake bool) {
	pending := pendingMesh{id: ref.id, generation: ref.generation, rebake: rebake}
	pending.vertices = l.appendStaging(input.vertices)
	pending.indices = l.appendStaging(input.indices)
	l.pendingMeshes = append(l.pendingMeshes, pending)
}

func (l *Lookup) appendStaging(data []byte) span {
	if len(data) == 0 {
		return span{}
	}
	at := len(l.staging)
	l.staging = append(l.staging, data...)
	return span{at: at, size: len(data)}
}

// drainMeshes applies everything the frame's callers queued: the uploads first,
// then the releases, so a mesh baked and released in one frame never reaches
// the GPU at all.
//
// An upload whose slot has moved on is dropped. The staging arena is handed to
// gfx rather than reused, because the bake takes the bytes without copying
// them; the next frame grows a fresh one.
func (l *Lookup) drainMeshes(baker meshBaker) {
	for _, pending := range l.pendingMeshes {
		record := &l.meshes[pending.id-1]
		if record.generation != pending.generation {
			continue
		}
		vertices, indices := pending.vertices.of(l.staging), pending.indices.of(l.staging)
		if pending.rebake && record.baked {
			record.vertices = baker.rebake(record.vertices, vertices)
			rebakeIndices(baker, record, indices)
		} else {
			record.vertices = baker.bake(vertices)
			record.indices, record.indexed = gfx.BufferDescr{}, len(indices) > 0
			if record.indexed {
				record.indices = baker.bake(indices)
			}
		}
		record.baked = true
	}
	l.pendingMeshes = l.pendingMeshes[:0]
	l.staging = nil
	for _, buffer := range l.pendingReleases {
		baker.release(buffer)
	}
	l.pendingReleases = l.pendingReleases[:0]
}

// rebakeIndices re-uploads a mesh's indices, minting or releasing the buffer
// when an update crosses between indexed and non-indexed geometry. A zero-length
// re-bake would leave a live buffer id on a mesh that no longer has indices, and
// gfx would then record it as indexed with nothing in it.
func rebakeIndices(baker meshBaker, record *meshRecord, indices []byte) {
	switch {
	case len(indices) == 0:
		if record.indexed {
			baker.release(record.indices)
		}
		record.indices, record.indexed = gfx.BufferDescr{}, false
	case !record.indexed:
		record.indices, record.indexed = baker.bake(indices), true
	default:
		record.indices = baker.rebake(record.indices, indices)
	}
}
