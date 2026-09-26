package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// indexedMesh is the smallest indexed mesh a translator test can draw: three
// vertices of the fake backend's 28-byte stride, and an index buffer of the
// given byte length declared at the given width.
func indexedMesh(topology types.PrimitiveTopology, width descriptors.IndexWidth, indexBytes int) descriptors.MeshDescr {
	const stride = 28
	return descriptors.MeshIndexed(
		descriptors.BufferWithBytes(make([]byte, 3*stride), true),
		descriptors.BufferWithBytes(make([]byte, indexBytes), true),
		width, topology,
		descriptors.Attr(0, descriptors.Float32x3),
		descriptors.Attr(12, descriptors.Float32x4),
	)
}

// The count is over indices, so it has to divide by the width the descriptor
// declares rather than by the four bytes gfx used to assume.
func TestIndexCountFollowsTheDeclaredWidth(t *testing.T) {
	for _, c := range []struct {
		name  string
		width descriptors.IndexWidth
		want  int
	}{
		{"uint16", descriptors.IndexUint16, 6},
		{"uint32", descriptors.IndexUint32, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			mesh := indexedMesh(types.TopologyTriangleList, c.width, 12)
			if mesh.IndexCount() != c.want {
				t.Fatalf("IndexCount() = %d over 12 bytes, want %d", mesh.IndexCount(), c.want)
			}
			if mesh.IndexWidth() != c.width {
				t.Fatalf("IndexWidth() = %v, want %v", mesh.IndexWidth(), c.width)
			}
		})
	}
}

// The zero value is the wide one, so a descriptor built without naming a width
// is wide rather than wrong.
func TestTheZeroIndexWidthIsUint32(t *testing.T) {
	var width descriptors.IndexWidth
	if width != descriptors.IndexUint32 || width.Bytes() != 4 {
		t.Fatalf("the zero IndexWidth is %v at %d bytes, want IndexUint32 at 4", width, width.Bytes())
	}
	if descriptors.IndexUint16.Bytes() != 2 {
		t.Fatalf("IndexUint16.Bytes() = %d, want 2", descriptors.IndexUint16.Bytes())
	}
}

// MeshIndexed is a pure value constructor with no error return, so the one
// thing gfx can check about an index buffer - that its bytes divide by the
// width it was declared at - is checked where the draw is translated.
func TestAnIndexBufferThatDoesNotDivideByItsWidthIsDroppedAndReportedOnce(t *testing.T) {
	backend := &fakeBackend{}
	// 13 bytes at two bytes an index: the last index is half a index.
	mesh := indexedMesh(types.TopologyTriangleList, descriptors.IndexUint16, 13)

	reported := pipelineErrFrames(t, backend, mesh, 3)

	if len(reported) != 1 {
		t.Fatalf("three frames reported %d errors, want 1: %v", len(reported), reported)
	}
	var length types.ErrIndexBufferLength
	if !errors.As(reported[0], &length) {
		t.Fatalf("reported %v, want ErrIndexBufferLength", reported[0])
	}
	if length.Length != 13 || length.Width != 2 {
		t.Errorf("report = %+v, want 13 bytes at a 2-byte width", length)
	}
	if len(backend.draws) != 0 {
		t.Errorf("%d draws were encoded, want none", len(backend.draws))
	}
	if backend.pipes != 0 {
		t.Errorf("the backend was asked for %d pipelines, want none for a dropped draw", backend.pipes)
	}
}

// The width the mesh declares is what the render pass binds the buffer at:
// binding uint16 indices as uint32 reads pairs of them as one index.
func TestTheDeclaredWidthReachesTheRenderPass(t *testing.T) {
	for _, c := range []struct {
		name  string
		width descriptors.IndexWidth
	}{{"uint16", descriptors.IndexUint16}, {"uint32", descriptors.IndexUint32}} {
		t.Run(c.name, func(t *testing.T) {
			backend := &fakeBackend{}
			if reported := pipelineErrFrames(t, backend, indexedMesh(types.TopologyTriangleList, c.width, 12), 1); len(reported) != 0 {
				t.Fatalf("the frame reported %v, want nothing", reported)
			}
			if len(backend.indexBinds) != 1 {
				t.Fatalf("%d index buffers were bound, want 1", len(backend.indexBinds))
			}
			if got := backend.indexBinds[0].width; got != c.width {
				t.Fatalf("the pass bound the index buffer at %v, want %v", got, c.width)
			}
		})
	}
}

// A strip pipeline declares the format that cuts the strip, so two strips of
// different widths are two pipelines.
func TestAStripIsKeyedByItsIndexWidth(t *testing.T) {
	backend := &fakeBackend{}
	narrow := indexedMesh(types.TopologyTriangleStrip, descriptors.IndexUint16, 12)
	wide := indexedMesh(types.TopologyTriangleStrip, descriptors.IndexUint32, 12)

	p := newPlugin()
	k := newTestKernel(t, p)
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	w, ref := recordList(t, k)
	w.Draw(ref, narrow, testMaterial(), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
	w.Draw(ref, wide, testMaterial(), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.pipes != 2 {
		t.Fatalf("two strips of different index widths built %d pipelines, want 2", backend.pipes)
	}
	if len(backend.lastPipelines) != 2 {
		t.Fatalf("the backend saw %d pipeline descriptors, want 2", len(backend.lastPipelines))
	}
	if a, b := backend.lastPipelines[0].IndexWidth, backend.lastPipelines[1].IndexWidth; a != descriptors.IndexUint16 || b != descriptors.IndexUint32 {
		t.Fatalf("the descriptors declared (%v, %v), want (IndexUint16, IndexUint32)", a, b)
	}
}

// A triangle list's pipeline never sees the index buffer, so keying on the
// width unconditionally would build two identical pipelines for two lists that
// differ only in an encoding detail.
func TestATriangleListIsNotKeyedByItsIndexWidth(t *testing.T) {
	backend := &fakeBackend{}
	narrow := indexedMesh(types.TopologyTriangleList, descriptors.IndexUint16, 12)
	wide := indexedMesh(types.TopologyTriangleList, descriptors.IndexUint32, 12)

	p := newPlugin()
	k := newTestKernel(t, p)
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	w, ref := recordList(t, k)
	w.Draw(ref, narrow, testMaterial(), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
	w.Draw(ref, wide, testMaterial(), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.pipes != 1 {
		t.Fatalf("two triangle lists of different index widths built %d pipelines, want 1", backend.pipes)
	}
	if len(backend.draws) != 2 {
		t.Fatalf("%d draws were encoded, want 2", len(backend.draws))
	}
}
