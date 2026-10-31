package internal

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

func TestTemporaryBufferUploadsOnceForEveryDrawThatBindsIt(t *testing.T) {
	b := newSetBench(t)
	arena := make([]byte, 3*types.StorageAlignment)
	arena[0] = 7

	buffer := b.queue.NewTemporaryBuffer(arena, true)
	arena[0] = 9
	pass := b.queue.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto()})
	for i := range 3 {
		b.queue.SetDrawParams(kernel.Kernel{}, b.set,
			types.ShaderParameterBufferRange("instances", buffer, i*types.StorageAlignment, types.StorageAlignment))
		b.queue.Draw(pass, b.mesh, b.set, 1, 0)
	}

	bakes := 0
	for _, op := range b.queue.resources {
		if op.Kind == OpBakeBuffer && op.BufferKind == types.BufferStorage {
			bakes++
			if op.Bytes[0] != 7 {
				t.Fatal("the temporary arena aliases caller data past the call")
			}
		}
	}
	if bakes != 1 {
		t.Fatalf("the arena uploaded %d times, want once for the whole frame", bakes)
	}
	b.backend.Execute(b.translate(t))
	var offsets []int
	for _, op := range b.backend.lastOps {
		if op.kind != testOpSetBuffer {
			continue
		}
		if op.buffer != buffer.ID {
			t.Fatalf("a draw bound buffer %d, want the one baked arena %d", op.buffer, buffer.ID)
		}
		offsets = append(offsets, op.offset)
	}
	if len(offsets) != 3 || offsets[1] != types.StorageAlignment || offsets[2] != 2*types.StorageAlignment {
		t.Errorf("draws bound the arena at %v, want each its own slice", offsets)
	}
}

func TestTemporaryTextureUploadsOnceForEveryDrawThatSamplesIt(t *testing.T) {
	b := newSetBench(t)
	pixels := []byte{7, 0, 0, 255}

	texture := b.queue.NewTemporaryTexture(1, 1, types.FormatRGBA8, pixels, true, false)
	pixels[0] = 9
	pass := b.queue.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto()})
	for range 3 {
		b.queue.SetDrawParams(kernel.Kernel{}, b.set, types.ShaderParameterTexture("albedo", texture))
		b.queue.Draw(pass, b.mesh, b.set, 1, 0)
	}

	bakes := 0
	for _, op := range b.queue.resources {
		if op.Kind == OpUpdateTexture {
			bakes++
			if op.Bytes[0] != 7 {
				t.Fatal("the temporary texture aliases caller pixels past the call")
			}
		}
	}
	if bakes != 1 {
		t.Fatalf("the texture uploaded %d times, want once for the whole frame", bakes)
	}
	b.backend.Execute(b.translate(t))
	bound := 0
	for _, op := range b.backend.lastOps {
		if op.kind == testOpSetTexture {
			bound++
			if op.texture != texture.Params.ID {
				t.Fatalf("a draw bound texture %d, want the one baked texture %d", op.texture, texture.Params.ID)
			}
		}
	}
	if bound != 3 {
		t.Errorf("texture binds = %d, want one a draw", bound)
	}

	if got := b.queue.NewTemporaryTexture(0, 1, types.FormatRGBA8, pixels, true, false); got.Params.ID != 0 {
		t.Fatalf("an empty texture baked as %d, want the zero descriptor", got.Params.ID)
	}
}

// No line number ever crosses the backend boundary as data: gogpu returns an
// internal parse-error type, so errors.As can never recover a line, and gfx must
// not import a backend's parser in any case. So gfx appends the rendered segment
// table of the flattened module and lets the reader subtract.
func TestABackendCompileFailureCarriesTheSegmentTable(t *testing.T) {
	filesystem := fstest.MapFS{
		"root.wgsl": &fstest.MapFile{Data: []byte("#include ./part.wgsl\nconst root = 1;")},
		"part.wgsl": &fstest.MapFile{Data: []byte("const part = 1;\nconst more = 2;")},
	}
	_, backend, k, reported := shaderEngine(t, filesystem)
	backend.createErr = errors.New("gogpu: parse error: line 3, column 12: expected ';'")

	compiled := compileShader(k, filesystem, shader.ShaderWithResource("root.wgsl", shader.ShaderDefine("HQ")))
	if compiled.Err != nil {
		t.Fatalf("compile: %v", compiled.Err)
	}
	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) { q.UploadProgram(k, q.NewShader(), compiled.Program) })
	render(k)

	if len(*reported) != 1 {
		t.Fatalf("the frame reported %d errors, want 1: %v", len(*reported), *reported)
	}
	var refused shader.ErrShaderSource
	if !errors.As((*reported)[0], &refused) {
		t.Fatalf("reported %v, want an ErrShaderSource", (*reported)[0])
	}
	if !errors.Is(refused.Err, backend.createErr) {
		t.Errorf("Err = %v, want the backend's own error unrewritten", refused.Err)
	}
	rendered := refused.Error()
	for _, want := range []string{
		`root.wgsl [HQ]`,
		"line 3, column 12",
		"flattened: 1-1 root.wgsl; 2-3 part.wgsl (root.wgsl:1); 4-4 root.wgsl@2",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("Error() = %q, missing %q", rendered, want)
		}
	}
}
