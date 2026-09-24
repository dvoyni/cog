package internal

// The friend functions: what scene's internal/ reads from, or does to, a public
// type's unexported state. What it reads of model's residency - the Lookup, a
// MeshRef, a selector's report key - it reads through their own exported
// methods, since model's internal is out of its reach. Only packages under
// bundles/scene can import this package, so these are not public API. Each is
// a field read or a direct call, so the flush pays nothing for going through
// one.

// LayerMaskDrawnBy calls LayerMask.drawnBy for scene's internal/.
func LayerMaskDrawnBy(v LayerMask, cull LayerMask) bool { return v.drawnBy(cull) }

// MaterialTagOf calls MaterialTag.tag for scene's internal/.
func MaterialTagOf(v MaterialTag) PassTag { return v.tag() }

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
