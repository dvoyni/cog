package types

import (
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/kernel"
)

// The friend functions: what scene's internal/ reads from, or does to, a public
// type's unexported state. Only packages under bundles/scene can import this
// package, so these are not public API. Each is a field read or a direct call, so the
// flush pays nothing for going through one.

// LayerMaskDrawnBy calls LayerMask.drawnBy for scene's internal/.
func LayerMaskDrawnBy(v LayerMask, cull LayerMask) bool { return v.drawnBy(cull) }

// LookupAccessLookup reads LookupAccess.lookup for scene's internal/'s tests.
func LookupAccessLookup(v LookupAccess) *Lookup { return v.lookup }

// LookupApplyUnloads calls Lookup.applyUnloads for scene's internal/.
func LookupApplyUnloads(v *Lookup, releaseTexture func(gfx.TextureDescr)) {
	v.applyUnloads(releaseTexture)
}

// LookupDefaults reads Lookup.defaults for scene's internal/'s tests.
func LookupDefaults(v *Lookup) PbrDefaults { return v.defaults }

// LookupDrainMeshes calls Lookup.drainMeshes for scene's internal/.
func LookupDrainMeshes(v *Lookup, baker MeshBaker) { v.drainMeshes(baker) }

// LookupEnsureBundled calls Lookup.ensureBundled for scene's internal/.
func LookupEnsureBundled(v *Lookup, bake bakeTextureFunc) [4]Material { return v.ensureBundled(bake) }

// LookupEnsureUnit calls Lookup.ensureUnit for scene's internal/.
func LookupEnsureUnit(v *Lookup, shape unitShape, bake BakeFunc) MeshRef {
	return v.ensureUnit(shape, bake)
}

// LookupInstallModel calls Lookup.installModel for scene's internal/.
func LookupInstallModel(
	v *Lookup, report func(error), path string, generation uint32,
	loaded *LoadedModel, failure error, resources *gfx.ResourceQueue,
) {
	v.installModel(report, path, generation, loaded, failure, resources)
}

// LookupMesh calls Lookup.mesh for scene's internal/.
func LookupMesh(v *Lookup, ref MeshRef) (MeshRecord, bool) { return v.mesh(ref) }

// LookupMeshes reads Lookup.meshes for scene's internal/'s tests.
func LookupMeshes(v *Lookup) []MeshRecord { return v.meshes }

// LookupModelEntry calls Lookup.modelEntry for scene's internal/'s tests.
func LookupModelEntry(v *Lookup, key string) *ModelEntry { return v.modelEntry(key) }

// LookupPendingMeshes reads Lookup.pendingMeshes for scene's internal/'s tests.
func LookupPendingMeshes(v *Lookup) []pendingMesh { return v.pendingMeshes }

// LookupReportOnce calls Lookup.reportOnce for scene's internal/.
func LookupReportOnce(v *Lookup, report func(error), key string, errs ...error) {
	v.reportOnce(report, key, errs...)
}

// LookupRequestModel calls Lookup.requestModel for scene's internal/.
func LookupRequestModel(v *Lookup, k kernel.Kernel, path string) (*ModelEntry, bool) {
	return v.requestModel(k, path)
}

// LookupStaging reads Lookup.staging for scene's internal/'s tests.
func LookupStaging(v *Lookup) []byte { return v.staging }

// MaterialTagOf calls MaterialTag.tag for scene's internal/.
func MaterialTagOf(v MaterialTag) PassTag { return v.tag() }

// MeshRefGeneration reads MeshRef.generation for scene's internal/.
func MeshRefGeneration(v MeshRef) uint32 { return v.generation }

// MeshRefIndex reads MeshRef.id for scene's internal/.
func MeshRefIndex(v MeshRef) uint32 { return v.id }

// MeshRefSource reads MeshRef.source for scene's internal/.
func MeshRefSource(v MeshRef) MeshSource { return v.source }

// OpQueueAppendDraw calls OpQueue.appendFlushDraw for scene's internal/.
func OpQueueAppendDraw(v *OpQueue, record DrawRecord) { v.appendFlushDraw(record) }

// OpQueueBeginFlush calls OpQueue.beginFlush for scene's internal/.
func OpQueueBeginFlush(v *OpQueue) []CameraRecord { return v.beginFlush() }

// OpQueueDraw calls OpQueue.draw for scene's internal/'s tests.
func OpQueueDraw(v *OpQueue, record DrawRecord) { v.draw(record) }

// OpQueueDrawCount calls OpQueue.drawCount for scene's internal/.
func OpQueueDrawCount(v *OpQueue) int { return v.drawCount() }

// OpQueueDuplicates calls OpQueue.recordedDuplicates for scene's internal/.
func OpQueueDuplicates(v *OpQueue) []CameraID { return v.recordedDuplicates() }

// OpQueueEndFlush calls OpQueue.endFlush for scene's internal/.
func OpQueueEndFlush(v *OpQueue) { v.endFlush() }

// OpQueueFlushDraws calls OpQueue.flushDraws for scene's internal/.
func OpQueueFlushDraws(v *OpQueue) []DrawRecord { return v.flushDraws() }

// OpQueueFlushLights calls OpQueue.flushLights for scene's internal/.
func OpQueueFlushLights(v *OpQueue) []LightRecord { return v.flushLights() }

// OpQueueFlushMeshes calls OpQueue.flushMeshes for scene's internal/.
func OpQueueFlushMeshes(v *OpQueue) *MeshRecording { return v.flushMeshes() }

// OpQueueFlushModels calls OpQueue.flushModels for scene's internal/.
func OpQueueFlushModels(v *OpQueue) []ModelDrawRecord { return v.flushModels() }

// OpQueuePublishBatches calls OpQueue.publishBatches for scene's internal/.
func OpQueuePublishBatches(v *OpQueue, batches []BatchView) { v.publishBatches(batches) }

// OpQueuePublishPass calls OpQueue.publishPass for scene's internal/.
func OpQueuePublishPass(v *OpQueue, view PassView) { v.publishPass(view) }

// OpQueuePublishedFrame reads OpQueue.publishedFrame for scene's internal/.
func OpQueuePublishedFrame(v *OpQueue) uint32 { return v.publishedFrame }

// OpQueueRecordedDraws reads OpQueue.draws for scene's internal/'s tests.
func OpQueueRecordedDraws(v *OpQueue) []DrawRecord { return v.draws }

// OpQueueRecordedMeshes reads OpQueue.meshes for scene's internal/'s tests.
func OpQueueRecordedMeshes(v *OpQueue) MeshRecording { return v.meshes }

// PassTagOf calls Pass.tag for scene's internal/.
func PassTagOf(v Pass) PassTag { return v.tag() }

// SelectorReportKey calls ModelSelectorError.reportKey for scene's internal/.
func SelectorReportKey(v ModelSelectorError) string { return v.reportKey() }
