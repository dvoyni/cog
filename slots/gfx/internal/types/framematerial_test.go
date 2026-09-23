package types

import "testing"

// frameMaterialQueue is a queue whose draws need no id: every param below is a
// value, so nothing is baked into a temporary.
func frameMaterialQueue() *OpQueue {
	q := NewOpQueue(nil)
	q.Reset()
	q.Pass(PassDescr{})
	return q
}

func frameMaterialParams() []ParameterDescr {
	return []ParameterDescr{FloatParam("a", 1), FloatParam("b", 2), FloatParam("c", 3)}
}

// A material recorded for the frame is copied into the queue once: every draw
// that names it binds that one copy, and the queue's arena grows by the draws'
// own params alone.
func TestAFrameMaterialIsCopiedOnceForEveryDrawOfIt(t *testing.T) {
	q := frameMaterialQueue()
	material := Material(ShaderWithText("s"), frameMaterialParams()...)
	recorded := q.FrameMaterial(material)
	afterRecord := len(q.parameterArena)
	if afterRecord != len(material.Params()) {
		t.Fatalf("recording put %d params in the arena, want the material's %d", afterRecord, len(material.Params()))
	}
	for range 3 {
		q.Draw(MeshDescr{}, recorded, FloatParam("d", 4))
	}
	if got := len(q.parameterArena) - afterRecord; got != 3 {
		t.Fatalf("three draws grew the arena by %d params, want their own three", got)
	}
	for i, op := range q.ops {
		if got := op.Material.params; len(got) != 3 || got[0].Name() != "a" || got[2].Name() != "c" {
			t.Errorf("draw %d binds %v, want the recorded params", i, ParameterViewsOf(got))
		}
	}
}

// The caller may reuse its own slice the moment FrameMaterial returns, which is
// the guarantee Draw gives: the recorded params are the queue's.
func TestAFrameMaterialOwesNothingToTheCallersSlice(t *testing.T) {
	q := frameMaterialQueue()
	params := frameMaterialParams()
	recorded := q.FrameMaterial(Material(ShaderWithText("s"), params...))
	params[0] = FloatParam("z", 9)
	q.Draw(MeshDescr{}, recorded)
	if name := q.ops[0].Material.params[0].Name(); name != "a" {
		t.Errorf("the draw binds %q, want the params as they were recorded", name)
	}
}

// A recorded material is the queue frame's. After the queue resets, and on
// any other queue, it draws exactly as the material it was recorded from:
// copied from the caller's params, as every material is.
func TestAStaleOrForeignFrameMaterialDrawsAsItsOriginal(t *testing.T) {
	q := frameMaterialQueue()
	params := frameMaterialParams()
	recorded := q.FrameMaterial(Material(ShaderWithText("s"), params...))

	other := frameMaterialQueue()
	other.Draw(MeshDescr{}, recorded)
	if got := other.ops[0].Material.params; len(got) != 3 || &got[0] == &q.parameterArena[0] || got[2].Name() != "c" {
		t.Errorf("another queue bound %v, want its own copy of the original params", ParameterViewsOf(got))
	}

	q.Reset()
	q.Pass(PassDescr{})
	// The arena's backing is reused by the new frame, so the recorded window
	// now holds this frame's params, not the material's.
	q.Draw(MeshDescr{}, Material(ShaderWithText("t"), FloatParam("x", 7), FloatParam("y", 8), FloatParam("w", 9)))
	q.Draw(MeshDescr{}, recorded)
	got := q.ops[1].Material.params
	if len(got) != 3 || got[0].Name() != "a" || got[2].Name() != "c" {
		t.Errorf("a stale recording bound %v, want the original params", ParameterViewsOf(got))
	}
	if &got[0] == &q.ops[0].Material.params[0] {
		t.Error("a stale recording bound the new frame's window")
	}
}

// A clone is an ordinary material again: its params are wherever it put them,
// not the queue's.
func TestACloneOfAFrameMaterialIsNotRecorded(t *testing.T) {
	q := frameMaterialQueue()
	recorded := q.FrameMaterial(Material(ShaderWithText("s"), frameMaterialParams()...))
	for name, clone := range map[string]MaterialDescr{
		"Clone":   recorded.Clone(),
		"CloneTo": func() MaterialDescr { c, _ := recorded.CloneTo(nil); return c }(),
	} {
		if _, ok := MaterialShapeState(&clone); ok {
			t.Errorf("%s kept the recording", name)
		}
	}
}

// The recorded shape state is exactly what hashing the params' names gives,
// so a plan cached through one is found through the other.
func TestAFrameMaterialsShapeStateIsItsParamsNames(t *testing.T) {
	q := frameMaterialQueue()
	material := Material(ShaderWithText("s"), frameMaterialParams()...)
	recorded := q.FrameMaterial(material)
	state, ok := MaterialShapeState(&recorded)
	if !ok {
		t.Fatal("a recorded material carries no shape state")
	}
	if want := ParameterShapeState(material.Params()); state != want {
		t.Errorf("the recorded state is %x, want the params' %x", state, want)
	}
	if _, ok := MaterialShapeState(&material); ok {
		t.Error("an unrecorded material claims a shape state")
	}
}
