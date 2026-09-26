package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"
)

// frameMaterialQueue is a queue whose draws need no id: every param below is a
// value, so nothing is baked into a temporary.
func frameMaterialQueue() (*OpQueue, descriptors.PassRef) {
	q := NewOpQueue(nil)
	q.reset()
	return q, q.NewPass(descriptors.PassDescr{})
}

func frameMaterialParams() []descriptors.ParameterDescr {
	return []descriptors.ParameterDescr{descriptors.FloatParam("a", 1), descriptors.FloatParam("b", 2), descriptors.FloatParam("c", 3)}
}

// A material recorded for the frame is copied into the queue once: every draw
// that names it binds that one copy, and the queue's arena grows by the draws'
// own params alone.
func TestAFrameMaterialIsCopiedOnceForEveryDrawOfIt(t *testing.T) {
	q, ref := frameMaterialQueue()
	material := descriptors.Material(shader.ShaderWithText("s"), frameMaterialParams()...)
	recorded := q.FrameMaterial(material)
	afterRecord := len(q.parameterArena)
	if afterRecord != len(material.Params()) {
		t.Fatalf("recording put %d params in the arena, want the material's %d", afterRecord, len(material.Params()))
	}
	for range 3 {
		q.Draw(ref, descriptors.MeshDescr{}, recorded, 1, 0, descriptors.FloatParam("d", 4))
	}
	if got := len(q.parameterArena) - afterRecord; got != 3 {
		t.Fatalf("three draws grew the arena by %d params, want their own three", got)
	}
	for i, op := range q.draws {
		if got := op.Material.Params(); len(got) != 3 || got[0].Name() != "a" || got[2].Name() != "c" {
			t.Errorf("draw %d binds %v, want the recorded params", i, ParameterViewsOf(got))
		}
	}
}

// The caller may reuse its own slice the moment FrameMaterial returns, which is
// the guarantee Draw gives: the recorded params are the queue's.
func TestAFrameMaterialOwesNothingToTheCallersSlice(t *testing.T) {
	q, ref := frameMaterialQueue()
	params := frameMaterialParams()
	recorded := q.FrameMaterial(descriptors.Material(shader.ShaderWithText("s"), params...))
	params[0] = descriptors.FloatParam("z", 9)
	q.Draw(ref, descriptors.MeshDescr{}, recorded, 1, 0)
	if name := q.draws[0].Material.Params()[0].Name(); name != "a" {
		t.Errorf("the draw binds %q, want the params as they were recorded", name)
	}
}

// A recorded material is the queue frame's. After the queue resets, and on
// any other queue, it draws exactly as the material it was recorded from:
// copied from the caller's params, as every material is.
func TestAStaleOrForeignFrameMaterialDrawsAsItsOriginal(t *testing.T) {
	q, ref := frameMaterialQueue()
	params := frameMaterialParams()
	recorded := q.FrameMaterial(descriptors.Material(shader.ShaderWithText("s"), params...))

	other, ref := frameMaterialQueue()
	other.Draw(ref, descriptors.MeshDescr{}, recorded, 1, 0)
	if got := other.draws[0].Material.Params(); len(got) != 3 || &got[0] == &q.parameterArena[0] || got[2].Name() != "c" {
		t.Errorf("another queue bound %v, want its own copy of the original params", ParameterViewsOf(got))
	}

	q.reset()
	ref = q.NewPass(descriptors.PassDescr{})
	// The arena's backing is reused by the new frame, so the recorded window
	// now holds this frame's params, not the material's.
	q.Draw(ref, descriptors.MeshDescr{}, descriptors.Material(shader.ShaderWithText("t"), descriptors.FloatParam("x", 7), descriptors.FloatParam("y", 8), descriptors.FloatParam("w", 9)), 1, 0)
	q.Draw(ref, descriptors.MeshDescr{}, recorded, 1, 0)
	got := q.draws[1].Material.Params()
	if len(got) != 3 || got[0].Name() != "a" || got[2].Name() != "c" {
		t.Errorf("a stale recording bound %v, want the original params", ParameterViewsOf(got))
	}
	if &got[0] == &q.draws[0].Material.Params()[0] {
		t.Error("a stale recording bound the new frame's window")
	}
}

// A clone is an ordinary material again: its params are wherever it put them,
// not the queue's.
func TestACloneOfAFrameMaterialIsNotRecorded(t *testing.T) {
	q, _ := frameMaterialQueue()
	recorded := q.FrameMaterial(descriptors.Material(shader.ShaderWithText("s"), frameMaterialParams()...))
	for name, clone := range map[string]descriptors.MaterialDescr{
		"Clone":   recorded.Clone(),
		"CloneTo": func() descriptors.MaterialDescr { c, _ := recorded.CloneTo(nil); return c }(),
	} {
		if _, ok := descriptors.MaterialShapeState(&clone); ok {
			t.Errorf("%s kept the recording", name)
		}
	}
}

// The recorded shape state is exactly what hashing the params' names gives,
// so a plan cached through one is found through the other.
func TestAFrameMaterialsShapeStateIsItsParamsNames(t *testing.T) {
	q, _ := frameMaterialQueue()
	material := descriptors.Material(shader.ShaderWithText("s"), frameMaterialParams()...)
	recorded := q.FrameMaterial(material)
	state, ok := descriptors.MaterialShapeState(&recorded)
	if !ok {
		t.Fatal("a recorded material carries no shape state")
	}
	if want := descriptors.ParameterShapeState(material.Params()); state != want {
		t.Errorf("the recorded state is %x, want the params' %x", state, want)
	}
	if _, ok := descriptors.MaterialShapeState(&material); ok {
		t.Error("an unrecorded material claims a shape state")
	}
}
