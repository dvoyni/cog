package internal

import (
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/kernel"
)

// The friend functions: what sceneimpl reads from, or does to, a public type's
// unexported state. Only the scene root and sceneimpl can import this package,
// so these are not public API. Each is a field read or a direct call, so the
// flush pays nothing for going through one.

// LayerMaskDrawnBy calls LayerMask.drawnBy for sceneimpl.
func LayerMaskDrawnBy(v LayerMask, cull LayerMask) bool { return v.drawnBy(cull) }

// LookupAccessLookup reads LookupAccess.lookup for sceneimpl's tests.
func LookupAccessLookup(v LookupAccess) *Lookup { return v.lookup }

// LookupApplyUnloads calls Lookup.applyUnloads for sceneimpl.
func LookupApplyUnloads(v *Lookup, releaseTexture func(gfx.TextureDescr)) {
	v.applyUnloads(releaseTexture)
}

// LookupDefaults reads Lookup.defaults for sceneimpl's tests.
func LookupDefaults(v *Lookup) PbrDefaults { return v.defaults }

// LookupDrainMeshes calls Lookup.drainMeshes for sceneimpl.
func LookupDrainMeshes(v *Lookup, baker MeshBaker) { v.drainMeshes(baker) }

// LookupEnsureBundled calls Lookup.ensureBundled for sceneimpl.
func LookupEnsureBundled(v *Lookup, bake bakeTextureFunc) [4]Material { return v.ensureBundled(bake) }

// LookupEnsureUnit calls Lookup.ensureUnit for sceneimpl.
func LookupEnsureUnit(v *Lookup, shape unitShape, bake BakeFunc) MeshRef {
	return v.ensureUnit(shape, bake)
}

// LookupInstallModel calls Lookup.installModel for sceneimpl.
func LookupInstallModel(
	v *Lookup, report func(error), path string, generation uint32,
	loaded *LoadedModel, failure error, resources *gfx.ResourceQueue,
) {
	v.installModel(report, path, generation, loaded, failure, resources)
}

// LookupMesh calls Lookup.mesh for sceneimpl.
func LookupMesh(v *Lookup, ref MeshRef) (MeshRecord, bool) { return v.mesh(ref) }

// LookupMeshes reads Lookup.meshes for sceneimpl's tests.
func LookupMeshes(v *Lookup) []MeshRecord { return v.meshes }

// LookupModelEntry calls Lookup.modelEntry for sceneimpl's tests.
func LookupModelEntry(v *Lookup, key string) *ModelEntry { return v.modelEntry(key) }

// LookupPendingMeshes reads Lookup.pendingMeshes for sceneimpl's tests.
func LookupPendingMeshes(v *Lookup) []pendingMesh { return v.pendingMeshes }

// LookupReportOnce calls Lookup.reportOnce for sceneimpl.
func LookupReportOnce(v *Lookup, report func(error), key string, errs ...error) {
	v.reportOnce(report, key, errs...)
}

// LookupRequestModel calls Lookup.requestModel for sceneimpl.
func LookupRequestModel(v *Lookup, k kernel.Kernel, path string) (*ModelEntry, bool) {
	return v.requestModel(k, path)
}

// LookupStaging reads Lookup.staging for sceneimpl's tests.
func LookupStaging(v *Lookup) []byte { return v.staging }

// MaterialTagOf calls MaterialTag.tag for sceneimpl.
func MaterialTagOf(v MaterialTag) PassTag { return v.tag() }

// MeshRefGeneration reads MeshRef.generation for sceneimpl.
func MeshRefGeneration(v MeshRef) uint32 { return v.generation }

// MeshRefIndex reads MeshRef.id for sceneimpl.
func MeshRefIndex(v MeshRef) uint32 { return v.id }

// MeshRefSource reads MeshRef.source for sceneimpl.
func MeshRefSource(v MeshRef) MeshSource { return v.source }

// OpQueueAppendDraw calls OpQueue.appendFlushDraw for sceneimpl.
func OpQueueAppendDraw(v *OpQueue, record DrawRecord) { v.appendFlushDraw(record) }

// OpQueueBeginFlush calls OpQueue.beginFlush for sceneimpl.
func OpQueueBeginFlush(v *OpQueue) []CameraRecord { return v.beginFlush() }

// OpQueueDraw calls OpQueue.draw for sceneimpl's tests.
func OpQueueDraw(v *OpQueue, record DrawRecord) { v.draw(record) }

// OpQueueDrawCount calls OpQueue.drawCount for sceneimpl.
func OpQueueDrawCount(v *OpQueue) int { return v.drawCount() }

// OpQueueDuplicates calls OpQueue.recordedDuplicates for sceneimpl.
func OpQueueDuplicates(v *OpQueue) []CameraID { return v.recordedDuplicates() }

// OpQueueEndFlush calls OpQueue.endFlush for sceneimpl.
func OpQueueEndFlush(v *OpQueue) { v.endFlush() }

// OpQueueFlushDraws calls OpQueue.flushDraws for sceneimpl.
func OpQueueFlushDraws(v *OpQueue) []DrawRecord { return v.flushDraws() }

// OpQueueFlushLights calls OpQueue.flushLights for sceneimpl.
func OpQueueFlushLights(v *OpQueue) []LightRecord { return v.flushLights() }

// OpQueueFlushMeshes calls OpQueue.flushMeshes for sceneimpl.
func OpQueueFlushMeshes(v *OpQueue) *MeshRecording { return v.flushMeshes() }

// OpQueueFlushModels calls OpQueue.flushModels for sceneimpl.
func OpQueueFlushModels(v *OpQueue) []ModelDrawRecord { return v.flushModels() }

// OpQueuePublishBatches calls OpQueue.publishBatches for sceneimpl.
func OpQueuePublishBatches(v *OpQueue, batches []BatchView) { v.publishBatches(batches) }

// OpQueuePublishPass calls OpQueue.publishPass for sceneimpl.
func OpQueuePublishPass(v *OpQueue, view PassView) { v.publishPass(view) }

// OpQueuePublishedFrame reads OpQueue.publishedFrame for sceneimpl.
func OpQueuePublishedFrame(v *OpQueue) uint32 { return v.publishedFrame }

// OpQueueRecordedDraws reads OpQueue.draws for sceneimpl's tests.
func OpQueueRecordedDraws(v *OpQueue) []DrawRecord { return v.draws }

// OpQueueRecordedMeshes reads OpQueue.meshes for sceneimpl's tests.
func OpQueueRecordedMeshes(v *OpQueue) MeshRecording { return v.meshes }

// PassTagOf calls Pass.tag for sceneimpl.
func PassTagOf(v Pass) PassTag { return v.tag() }

// SelectorReportKey calls ModelSelectorError.reportKey for sceneimpl.
func SelectorReportKey(v ModelSelectorError) string { return v.reportKey() }
