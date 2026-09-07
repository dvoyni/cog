package scene

import (
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
)

// skinnedModel is the smallest animated file: one triangle on a node a clip
// spins, which is a degenerate single-joint skin and the ordinary way glTF
// authors a wheel or a door.
func skinnedModel(t testing.TB) *gltf.Document {
	t.Helper()
	doc := testDoc()
	triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Name: "wheel", Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	rotationClip(doc, "spin", 0, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	return doc
}

// instancesOf decodes the instance records one binding covered, which is the
// only place the flags and the animation offset a frame packed are readable.
func instancesOf(t *testing.T, h *harness, binding bufferBinding) []sceneInstance {
	t.Helper()
	data, ok := h.backend.baked[binding.buffer]
	if !ok {
		t.Fatalf("buffer %v was bound but never uploaded", binding.buffer)
	}
	if binding.offset+binding.size > len(data) {
		t.Fatalf("the binding covers %d..%d of a %d-byte buffer",
			binding.offset, binding.offset+binding.size, len(data))
	}
	size := int(unsafe.Sizeof(sceneInstance{}))
	instances := make([]sceneInstance, binding.size/size)
	for i := range instances {
		at := binding.offset + i*size
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&instances[i])), size), data[at:at+size])
	}
	return instances
}

// boundBytes reports the whole contents of the buffer bound to one name on the
// frame's first draw. Group 2 is bound whole rather than as a range - the
// records are addressed by index, not by binding - so the size the backend
// records is zero and the buffer's own length is the assertion.
func boundBytes(t *testing.T, h *harness, name string) []byte {
	t.Helper()
	bound := h.backend.buffersBoundTo(name)
	if len(bound) == 0 {
		t.Fatalf("nothing bound %s; an unbound declared binding kills the frame", name)
	}
	data, ok := h.backend.baked[bound[0].buffer]
	if !ok {
		t.Fatalf("%s was bound to buffer %v, which was never uploaded", name, bound[0].buffer)
	}
	return data
}

// firstInstance decodes the first instance the frame packed.
func firstInstance(t *testing.T, h *harness) sceneInstance {
	t.Helper()
	bound := h.backend.buffersBoundTo("sceneInstances")
	if len(bound) == 0 {
		t.Fatal("nothing bound sceneInstances")
	}
	instances := instancesOf(t, h, bound[0])
	if len(instances) == 0 {
		t.Fatal("the pass bound an empty instance slice")
	}
	return instances[0]
}

// Group 2 is declared on the one module every draw goes through, and a
// declared binding that nothing binds does not degrade the frame - it takes the
// whole command buffer down silently. So a debug box binds a skin too.
func TestAnUnskinnedDrawBindsTheNullSkin(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(cameraMain, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
	})
	h.frame()
	for _, name := range []string{"scenePoses", "sceneSkinJoints", "sceneAnim"} {
		if bound := h.backend.buffersBoundTo(name); len(bound) == 0 {
			t.Errorf("%s was not bound; an unbound declared binding kills the frame", name)
		}
	}
	instance := firstInstance(t, h)
	if instance.Flags&sceneNoSkin == 0 {
		t.Error("a debug box carries SCENE_NOSKIN")
	}
	if instance.AnimOffset != sceneNoAnim {
		t.Errorf("AnimOffset = %d, want sceneNoAnim", instance.AnimOffset)
	}
}

// Every unskinned draw shares one null skin, which is what keeps them batching
// together instead of fragmenting group 2 a bind group at a time.
func TestEveryUnskinnedDrawSharesOneNullSkin(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(cameraMain, testCameraDescr())
		for i := range 4 {
			q.Box(0, At(float32(i), 0, 0), testBoxColor)
		}
	})
	h.frame()
	bound := h.backend.buffersBoundTo("scenePoses")
	if len(bound) < 4 {
		t.Fatalf("scenePoses was bound %d times, want once per draw", len(bound))
	}
	for i, binding := range bound {
		if binding.buffer != bound[0].buffer {
			t.Errorf("draw %d binds pose buffer %v, want the one shared null skin %v",
				i, binding.buffer, bound[0].buffer)
		}
	}
	// One identity pose row and one identity joint, and nothing more.
	if got := len(boundBytes(t, h, "scenePoses")); got != poseSize {
		t.Errorf("the null skin's pose buffer is %d bytes, want one row of %d", got, poseSize)
	}
	if got := len(boundBytes(t, h, "sceneSkinJoints")); got != skinJointSize {
		t.Errorf("the null skin's joint buffer is %d bytes, want one record of %d", got, skinJointSize)
	}
}

// A model with joints binds its own baked records rather than the null skin,
// and its primitives are marked skinned so the shader follows the pose path.
func TestASkinnedModelBindsItsOwnPosesAndSkins(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))),
		drawModel(modelPath, ModelDraw{Plays: []ClipPlay{{Clip: "spin", Time: 0.5, Weight: 1}}}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	// One joint over 61 frames plus the rest frame. The null skin is one row,
	// so the size is what separates "bound the model" from "bound the null skin
	// and animates nothing".
	if got, want := len(boundBytes(t, h, "scenePoses")), 62*poseSize; got != want {
		t.Errorf("the bound pose buffer is %d bytes, want the model's %d", got, want)
	}
	if got := len(boundBytes(t, h, "sceneSkinJoints")); got != skinJointSize {
		t.Errorf("the joint buffer is %d bytes, want the model's one joint at %d", got, skinJointSize)
	}
	instance := firstInstance(t, h)
	if instance.Flags&sceneNoSkin != 0 {
		t.Error("a skinned model draw must not carry SCENE_NOSKIN")
	}
	if instance.AnimOffset == sceneNoAnim {
		t.Error("a draw with a play carries the offset of its sceneAnim block")
	}
}

// A model draw with no plays is the rest pose, which is a real pose: row 0 is
// the authored hierarchy resolved once. Without it the model would collapse to
// the origin, because a degenerate node's transform lives in the pose buffer.
func TestAModelDrawWithNoPlaysCarriesNoAnimBlock(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))),
		drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	instance := firstInstance(t, h)
	if instance.AnimOffset != sceneNoAnim {
		t.Errorf("AnimOffset = %d, want sceneNoAnim: no plays is the rest frame", instance.AnimOffset)
	}
	// Still skinned. The node's transform is in the pose buffer, so the draw
	// has to read row 0 rather than skip the path.
	if instance.Flags&sceneNoSkin != 0 {
		t.Error("a model whose node is a joint is skinned even with no plays")
	}
}

// A skinned draw is never culled. Its bind-pose sphere is the only bound the
// load has, and where the joints put it this frame is not knowable without
// replaying the blend on the CPU - which is the per-frame hierarchy walk the
// whole design exists to remove.
func TestASkinnedDrawIsNeverCulled(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))), func(q *OpQueue) {
		// A camera pointed the other way: the draw survives only because it is
		// exempt, not because it is in frustum.
		q.Camera(cameraMain, CameraDescr{
			Transform: LookAt(m.Vec3{Z: 500}, m.Vec3{Z: 1000}, m.Vec3{Y: 1}),
			FovY:      1.0472, Near: 0.1, Far: 200,
		})
		q.Model(LayersAll, modelPath, ModelDraw{})
	})
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Recorded == 1
	})
	if got := h.passes()[0].Culled; got != 0 {
		t.Errorf("culled %d draws, want none: a skinned draw is exempt", got)
	}
}

// The clip table and joint names are what a caller reads to drive an animation
// at all, and both trigger the load the way every other query does.
func TestTheLookupReportsClipsJointsAndPoseBytes(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))), func(*OpQueue) {})
	var clips []ClipInfo
	var joints []string
	var bytes int
	h.frameUntil(t, "the model to become resident", func() bool {
		h.lookup(func(access LookupAccess) {
			clips, _ = access.Clips(modelPath, clips[:0])
			joints, _ = access.Joints(modelPath, joints[:0])
			bytes, _ = access.PoseBytes(modelPath)
		})
		return len(clips) > 0
	})
	if len(clips) != 1 || clips[0].Name != "spin" {
		t.Fatalf("clips = %v, want the file's one clip", clips)
	}
	// The authored duration, not the grid's rounded-up one: a caller timing a
	// one-shot needs the length the artist gave it.
	if clips[0].Duration != 1 {
		t.Errorf("duration = %v, want the authored second", clips[0].Duration)
	}
	if len(joints) != 1 || joints[0] != "wheel" {
		t.Errorf("joints = %v, want the one degenerate joint", joints)
	}
	if want := 62 * poseSize; bytes != want {
		t.Errorf("PoseBytes = %d, want %d", bytes, want)
	}
	h.lookup(func(access LookupAccess) {
		if got := access.TotalPoseBytes(); got != bytes {
			t.Errorf("TotalPoseBytes = %d, want the one resident model's %d", got, bytes)
		}
	})
}

// A static prop bakes no poses at all, which is what puts it on the null skin
// and off the per-vertex pose path entirely.
func TestAStaticModelBakesNoPosesAndBindsTheNullSkin(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, onePrimitiveModel(t))),
		drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	h.lookup(func(access LookupAccess) {
		if got, _ := access.PoseBytes(modelPath); got != 0 {
			t.Errorf("PoseBytes = %d, want none for a file with no animation", got)
		}
	})
	if instance := firstInstance(t, h); instance.Flags&sceneNoSkin == 0 {
		t.Error("a static model carries SCENE_NOSKIN")
	}
	// The one pose binding in the frame is the null skin's single row.
	if got := len(boundBytes(t, h, "scenePoses")); got != poseSize {
		t.Errorf("scenePoses is %d bytes, want the null skin's one row of %d", got, poseSize)
	}
}

// A play naming a clip the file does not declare is dropped and reported once,
// however many frames draw it: one typo must not spawn a report per frame
// forever.
func TestAnUnknownClipReportsOnce(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))),
		drawModel(modelPath, ModelDraw{Plays: []ClipPlay{{Clip: "gallop", Weight: 1}}}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	for range 3 {
		h.frame()
	}
	missing := 0
	for _, err := range h.errors() {
		if _, ok := err.(ErrModelClipMissing); ok {
			missing++
		}
	}
	if missing != 1 {
		t.Fatalf("reported %d missing clips over four frames, want one", missing)
	}
	// The draw still renders, at the rest pose.
	if got := h.passes()[0].Instances; got != 1 {
		t.Errorf("instances = %d; a typo'd clip costs the play, not the model", got)
	}
}
