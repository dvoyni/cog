package internal

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// programLayout is the hand-written layout the fake reflection port answers
// with: one uniform block, a sampler and a texture, declared out of name order
// so the table's own ordering is what a lookup relies on.
func programLayout() shader.ShaderLayout {
	return shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "tint", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 1, Size: 16},
			{Name: "frame", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
			{Name: "albedo", Group: 1, Binding: 1},
			{Name: "albedoSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
		},
	}
}

// shaderEngine composes gfx over a fake whose reflection port answers with
// programLayout, and collects what the engine reports.
func shaderEngine(t *testing.T, filesystem fs.FS) (*plugin, *fakeBackend, kernel.Executioner, *[]error) {
	t.Helper()
	p := newPlugin()
	layout := programLayout()
	backend := &fakeBackend{reflection: &layout}
	reported := &[]error{}
	k := newTestKernelWith(t, p, filesystem, func(err error) error {
		*reported = append(*reported, err)
		return nil
	})
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	return p, backend, k, reported
}

func compileShader(k kernel.Executioner, filesystem fs.FS, descr shader.ShaderDescr) CompileShaderResponse {
	return k.ExecuteCommand[CompileShaderCmd](CompileShaderRequest{FS: filesystem, Descr: descr})
}

// withShaders runs use against the ResourceQueue with the dispatch's Kernel, so
// the calls that report a mistake have one to report through.
func withShaders(k kernel.Executioner, use func(kernel.Kernel, *ResourceQueue)) {
	k.ExecuteCommand[recordResourcesCmd](recordResourcesRequest{withKernel: use})
}

func render(k kernel.Executioner) { k.PublishEvent(app.RenderEvent{}).Wait() }

const programSource = "// the fake reflects whatever it is handed\n@vertex fn vs_main() {}\n"

// CompileShaderCmd reflects the flattened module through the backend's port and
// tables the result by WGSL global name. gfx imports no shader compiler: what
// the port answers is the layout, whatever the source says.
func TestCompileShaderTablesThePortsLayoutByName(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})

	response := compileShader(k, nil, shader.ShaderWithText(programSource))

	if response.Err != nil {
		t.Fatalf("compile failed: %v", response.Err)
	}
	program := response.Program
	if !program.Valid() || program.Label() != "gfx.shader" {
		t.Fatalf("program valid=%v label=%q, want a valid gfx.shader", program.Valid(), program.Label())
	}
	if len(backend.reflected) != 1 || !strings.Contains(backend.reflected[0], "@vertex fn vs_main") {
		t.Fatalf("the port was asked about %q, want the flattened module once", backend.reflected)
	}
	for _, want := range programLayout().Resources {
		got, ok := program.Binding(want.Name)
		if !ok || got.Group != want.Group || got.Binding != want.Binding || got.Kind != want.Kind || got.Size != want.Size {
			t.Errorf("Binding(%q) = %+v, %v, want %+v", want.Name, got, ok, want)
		}
	}
	if _, ok := program.Binding("missing"); ok {
		t.Error("Binding(missing) found a binding the shader never declared")
	}
	if len(*reported) != 0 {
		t.Errorf("a compile that succeeded reported %v", *reported)
	}
}

// The root and its includes come from the filesystem the request carries, and
// the program remembers every path the flatten read.
func TestCompileShaderReadsTheRootAndItsIncludesFromTheRequest(t *testing.T) {
	files := fstest.MapFS{
		"shaders/main.wgsl":   {Data: []byte("#include ./common.wgsl\n@vertex fn vs_main() {}\n")},
		"shaders/common.wgsl": {Data: []byte("fn helper() {}\n")},
	}
	_, backend, k, _ := shaderEngine(t, files)

	response := compileShader(k, files, shader.ShaderWithResource("shaders/main.wgsl"))

	if response.Err != nil {
		t.Fatalf("compile failed: %v", response.Err)
	}
	if got := response.Program.Sources(); len(got) != 2 || got[0] != "shaders/main.wgsl" || got[1] != "shaders/common.wgsl" {
		t.Errorf("sources = %q, want the root then its include", got)
	}
	if code := backend.reflected[0]; !strings.Contains(code, "fn helper()") || !strings.Contains(code, "fn vs_main()") {
		t.Errorf("the port reflected %q, want the root with its include spliced in", code)
	}
}

// A compile's failure is a normal outcome: it is the response's Err, beside
// the zero program, and it is never also reported.
func TestCompileShaderReturnsItsFailuresAndReportsNone(t *testing.T) {
	refused := errors.New("unexpected token")
	cases := []struct {
		name  string
		descr shader.ShaderDescr
		setup func(*fakeBackend)
		want  func(error) bool
	}{
		{
			name:  "a missing root",
			descr: shader.ShaderWithResource("shaders/absent.wgsl"),
			want:  func(err error) bool { return errors.Is(err, fs.ErrNotExist) },
		},
		{
			name:  "an include that does not resolve",
			descr: shader.ShaderWithText("#include absent/lib.wgsl\n"),
			want:  func(err error) bool { var source shader.ErrShaderSource; return errors.As(err, &source) },
		},
		{
			name:  "WGSL the port refuses",
			descr: shader.ShaderWithText(programSource),
			setup: func(b *fakeBackend) { b.reflectErr = refused },
			want:  func(err error) bool { return errors.Is(err, refused) },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := fstest.MapFS{}
			_, backend, k, reported := shaderEngine(t, files)
			if c.setup != nil {
				c.setup(backend)
			}

			response := compileShader(k, files, c.descr)

			if response.Err == nil || !c.want(response.Err) {
				t.Errorf("Err = %v, want %s", response.Err, c.name)
			}
			if response.Program.Valid() {
				t.Error("a failed compile returned a valid program")
			}
			if len(*reported) != 0 {
				t.Errorf("a failed compile reported %v, want it returned only", *reported)
			}
		})
	}
}

// The command declares no lock, which is what lets the kernel run it on the
// caller's goroutine and costs a System that uses it no parallelism.
func TestCompileShaderDeclaresNoLock(t *testing.T) {
	if lock, _ := newPlugin().compileShaderCmdImpl(); lock != nil {
		t.Error("CompileShaderCmd declares a lock; it must declare none")
	}
}

// NewShader hands an id back at once and UploadProgram gives the CPU side the
// program's table in the same call; the module itself is created when the
// render thread replays the upload.
func TestAnUploadedProgramIsTabledAtOnceAndCreatedAtReplay(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program

	var id types.ShaderID
	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		id = q.NewShader()
		q.UploadProgram(k, id, program)
		if got, ok := q.shaderProgram(id); !ok || got != program {
			t.Errorf("the queue holds %v, %v for the uploaded shader, want its program", got, ok)
		}
	})
	if id == 0 {
		t.Fatal("NewShader returned the zero id")
	}
	if len(backend.createdShaders) != 0 {
		t.Fatalf("the module was created before any replay: %+v", backend.createdShaders)
	}

	render(k)
	render(k)

	if len(backend.createdShaders) != 1 {
		t.Fatalf("created %d modules over two frames, want the upload replayed once", len(backend.createdShaders))
	}
	created := backend.createdShaders[0]
	if created.id != id || created.label != "gfx.shader" || created.code != backend.reflected[0] {
		t.Errorf("created %+v, want shader %d from the module that was reflected", created, id)
	}
	if len(*reported) != 0 {
		t.Errorf("reported %v", *reported)
	}
}

// A shader takes one upload. A second is reported once, however often it is
// repeated, and the module the first created is the only one.
func TestASecondUploadIsReportedOnceAndIgnored(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program

	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		id := q.NewShader()
		for range 3 {
			q.UploadProgram(k, id, program)
		}
	})
	render(k)

	if len(backend.createdShaders) != 1 {
		t.Errorf("created %d modules, want the first upload's only", len(backend.createdShaders))
	}
	var twice types.ErrShaderUploadedTwice
	if len(*reported) != 1 || !errors.As((*reported)[0], &twice) || twice.Label != "gfx.shader" {
		t.Fatalf("reported %v, want one ErrShaderUploadedTwice naming the program held", *reported)
	}
}

// The zero program - what a failed compile returns - is reported and ignored,
// and the shader stays reserved for a program that compiled.
func TestAnInvalidProgramIsReportedAndTheShaderStaysReserved(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program

	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		id := q.NewShader()
		q.UploadProgram(k, id, shader.ShaderProgram{})
		q.UploadProgram(k, id, program)
	})
	render(k)

	var invalid types.ErrShaderProgramInvalid
	if len(*reported) != 1 || !errors.As((*reported)[0], &invalid) {
		t.Fatalf("reported %v, want one ErrShaderProgramInvalid", *reported)
	}
	if len(backend.createdShaders) != 1 {
		t.Errorf("created %d modules, want the valid upload's", len(backend.createdShaders))
	}
}

// An id NewShader never reserved, and one already released, are programmer
// mistakes: each call naming one is reported once and ignored.
func TestACallNamingAShaderThatIsNotLiveIsReported(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program

	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		q.UploadProgram(k, 9999, program)
		q.ReleaseShader(k, 0)
		released := q.NewShader()
		q.ReleaseShader(k, released)
		q.UploadProgram(k, released, program)
		q.UploadProgram(k, released, program)
		q.ReleaseShader(k, released)
	})
	render(k)

	want := []types.ErrShaderNotLive{
		{Shader: 9999, Call: "UploadProgram"},
		{Shader: 0, Call: "ReleaseShader"},
		{Call: "UploadProgram", Released: true},
		{Call: "ReleaseShader", Released: true},
	}
	if len(*reported) != len(want) {
		t.Fatalf("reported %v, want %d ErrShaderNotLive", *reported, len(want))
	}
	for i, err := range *reported {
		var got types.ErrShaderNotLive
		if !errors.As(err, &got) || got.Call != want[i].Call || got.Released != want[i].Released ||
			(want[i].Shader != 0 && got.Shader != want[i].Shader) {
			t.Errorf("report %d = %v, want %+v", i, err, want[i])
		}
	}
	if len(backend.createdShaders) != 0 || len(backend.freedShaders) != 0 {
		t.Errorf("created %v and freed %v, want nothing reaching the backend", backend.createdShaders, backend.freedShaders)
	}
}

// ReleaseShader frees the module and sweeps every pipeline built on it, and the
// CPU side forgets the program at once.
func TestReleaseShaderFreesTheModuleAndItsPipelines(t *testing.T) {
	p, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program

	var id types.ShaderID
	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		id = q.NewShader()
		q.UploadProgram(k, id, program)
	})
	render(k)
	// No draw builds a pipeline on an explicit shader yet, so two are planted:
	// one built on it and one failed build, which is cached as zero and hands
	// nothing back.
	p.translator.pipelines[pipelineKey{shader: id}] = 77
	p.translator.pipelines[pipelineKey{shader: id, noColor: true}] = 0
	p.translator.pipelines[pipelineKey{shader: id + 1000}] = 78

	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		q.ReleaseShader(k, id)
		if _, ok := q.shaderProgram(id); ok {
			t.Error("the queue still holds the released shader's program")
		}
	})
	render(k)

	if len(backend.freedShaders) != 1 || backend.freedShaders[0] != id {
		t.Errorf("freed shaders %v, want %d", backend.freedShaders, id)
	}
	if len(backend.freedPipelines) != 1 || backend.freedPipelines[0] != 77 {
		t.Errorf("freed pipelines %v, want the one built on the shader", backend.freedPipelines)
	}
	if len(p.translator.pipelines) != 1 {
		t.Errorf("pipelines left %v, want only the other shader's", p.translator.pipelines)
	}
	if _, ok := p.translator.layouts[id]; ok {
		t.Error("the released shader's layout is still cached")
	}
	if len(*reported) != 0 {
		t.Errorf("reported %v", *reported)
	}
}

// A shader released before any upload never reached the GPU, so its release
// reaches nothing there either.
func TestAShaderReleasedBeforeItsUploadFreesNothing(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})

	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) { q.ReleaseShader(k, q.NewShader()) })
	render(k)

	if len(backend.freedShaders) != 0 || len(*reported) != 0 {
		t.Errorf("freed %v and reported %v, want neither", backend.freedShaders, *reported)
	}
}

// A module the backend refuses at replay is reported with its segment table,
// exactly as a descriptor's module is.
func TestAModuleTheBackendRefusesIsReportedAtReplay(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	refused := errors.New("device lost")
	backend.createErr = refused
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program

	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) { q.UploadProgram(k, q.NewShader(), program) })
	render(k)
	render(k)

	var source shader.ErrShaderSource
	if len(*reported) != 1 || !errors.As((*reported)[0], &source) || !errors.Is((*reported)[0], refused) {
		t.Fatalf("reported %v, want the refusal once as an ErrShaderSource", *reported)
	}
	if source.Shader != "gfx.shader" || !strings.Contains(source.Error(), "flattened: ") {
		t.Errorf("reported %q, want the label and the segment table", source.Error())
	}
}

// A program over the web floor is still created, and says so once: the upload
// is replayed once, so the diagnostic is too.
func TestAProgramOverTheWebFloorIsReportedOnceAtReplay(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	layout := shader.ShaderLayout{}
	for i := range 9 {
		layout.Resources = append(layout.Resources, shader.ShaderResource{
			Name: "records" + string(rune('a'+i)), Kind: shader.ResourceStorageBuffer, Group: 0, Binding: i,
		})
	}
	backend.reflection = &layout
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program

	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) { q.UploadProgram(k, q.NewShader(), program) })
	render(k)
	render(k)

	var exceeded shader.ErrShaderExceedsWebLimits
	if len(*reported) != 1 || !errors.As((*reported)[0], &exceeded) || exceeded.Declared != 9 {
		t.Fatalf("reported %v, want one ErrShaderExceedsWebLimits for 9 storage buffers", *reported)
	}
	if len(backend.createdShaders) != 1 {
		t.Errorf("created %d modules, want the shader kept", len(backend.createdShaders))
	}
}

// Shader ids come from the backend's own counter and are never reused: a
// released id is not handed out again.
func TestShaderIDsAreNeverReused(t *testing.T) {
	_, _, k, _ := shaderEngine(t, fstest.MapFS{})

	seen := map[types.ShaderID]bool{}
	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		for range 4 {
			id := q.NewShader()
			if seen[id] {
				t.Errorf("NewShader handed out %d twice", id)
			}
			seen[id] = true
			q.ReleaseShader(k, id)
		}
	})
}

// A frame snapshot names a shader's upload and release, and what it knows the
// shader by, without carrying the module's text.
func TestShaderOpsAreNamedInTheSnapshot(t *testing.T) {
	_, backend, k, _ := shaderEngine(t, fstest.MapFS{})
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program

	upload := resourceOpViewOf("durable", 0, &ResourceOp{Kind: OpUploadProgram, ShaderID: 7, Program: program})
	release := resourceOpViewOf("durable", 1, &ResourceOp{Kind: OpReleaseShader, ShaderID: 7})

	if upload.Kind != "uploadProgram" || upload.Shader != 7 || upload.Label != "gfx.shader" || upload.Bytes != len(backend.reflected[0]) {
		t.Errorf("upload view = %+v, want uploadProgram of shader 7 labelled gfx.shader", upload)
	}
	if release.Kind != "releaseShader" || release.Shader != 7 {
		t.Errorf("release view = %+v, want releaseShader of shader 7", release)
	}
}
