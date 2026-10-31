package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// shaderState is where one shader id is in its life. The zero state is an id
// NewShader never reserved, which is what a gap in the table reads as.
type shaderState uint8

const (
	shaderUnknown shaderState = iota
	shaderReserved
	shaderUploaded
	shaderReleased
)

// shaderRecord is the CPU side of one shader: its state, and the program it was
// uploaded with, whose binding table a set built on it resolves against the
// moment it is created rather than a replay later.
type shaderRecord struct {
	state   shaderState
	program shader.ShaderProgram
}

// shaderFault is one of the programmer mistakes a shader call can make.
type shaderFault uint8

const (
	shaderFaultNotLive shaderFault = iota
	shaderFaultInvalidProgram
	shaderFaultUploadedTwice
)

// shaderReportKey is the report-once key of one mistake about one shader. Ids
// are never reused, so the key names exactly one shader for the engine's life,
// and a mistake repeated every frame is said once.
type shaderReportKey struct {
	shader types.ShaderID
	fault  shaderFault
	call   string
}

// NewShader reserves a shader and returns its id at once. Nothing reaches the
// GPU until UploadProgram gives it a program, which also fixes it for good.
//
// It is the shader twin of NewBuffer: the id is minted CPU-side, from a counter
// that never reuses one, so a set can name the shader in the same tick it was
// reserved.
func (q *ResourceQueue) NewShader() types.ShaderID {
	id := q.ids().ReserveShader()
	if int(id) >= len(q.shaders) {
		q.shaders = append(q.shaders, make([]shaderRecord, int(id)+1-len(q.shaders))...)
	}
	q.shaders[id] = shaderRecord{state: shaderReserved}
	return id
}

// UploadProgram queues a reserved shader's module creation from program. The
// CPU side keeps the program's binding table from this call on; the backend
// creates the module when the render thread replays the upload.
//
// A shader takes one program for its life. A second upload, a program that
// failed to compile, and an id that is not live are programmer mistakes: each
// is reported through k once and the call is ignored. To reload a shader,
// release it and create it again.
func (q *ResourceQueue) UploadProgram(k kernel.Kernel, id types.ShaderID, program shader.ShaderProgram) {
	record, ok := q.liveShader(k, id, "UploadProgram")
	if !ok {
		return
	}
	if !program.Valid() {
		k.ReportErrorOnce(shaderReportKey{shader: id, fault: shaderFaultInvalidProgram},
			types.ErrShaderProgramInvalid{Shader: id})
		return
	}
	if record.state == shaderUploaded {
		k.ReportErrorOnce(shaderReportKey{shader: id, fault: shaderFaultUploadedTwice},
			types.ErrShaderUploadedTwice{Shader: id, Label: record.program.Label()})
		return
	}
	*record = shaderRecord{state: shaderUploaded, program: program}
	q.ops = append(q.ops, ResourceOp{Kind: OpUploadProgram, ShaderID: id, Program: program})
}

// ReleaseShader queues a durable release for a shader: its module, and every
// pipeline built on it. A shader released before any upload never reached the
// GPU and releases nothing there. An id that is not live is reported through k
// once and ignored.
func (q *ResourceQueue) ReleaseShader(k kernel.Kernel, id types.ShaderID) {
	record, ok := q.liveShader(k, id, "ReleaseShader")
	if !ok {
		return
	}
	uploaded := record.state == shaderUploaded
	// The program is dropped rather than kept beside the released state, so a
	// released shader holds nothing a collector would have to keep alive.
	*record = shaderRecord{state: shaderReleased}
	if uploaded {
		q.ops = append(q.ops, ResourceOp{Kind: OpReleaseShader, ShaderID: id})
	}
}

// liveShader returns the record of a reserved or uploaded shader, reporting
// once under call when the id names one never reserved or already released.
func (q *ResourceQueue) liveShader(k kernel.Kernel, id types.ShaderID, call string) (*shaderRecord, bool) {
	var state shaderState
	if int(id) < len(q.shaders) {
		state = q.shaders[id].state
	}
	if state == shaderReserved || state == shaderUploaded {
		return &q.shaders[id], true
	}
	k.ReportErrorOnce(shaderReportKey{shader: id, fault: shaderFaultNotLive, call: call},
		types.ErrShaderNotLive{Shader: id, Call: call, Released: state == shaderReleased})
	return nil, false
}

// shaderProgram returns the program an uploaded, unreleased shader holds, which
// is what a set built on it resolves its params against.
func (q *ResourceQueue) shaderProgram(id types.ShaderID) (shader.ShaderProgram, bool) {
	if int(id) >= len(q.shaders) || q.shaders[id].state != shaderUploaded {
		return shader.ShaderProgram{}, false
	}
	return q.shaders[id].program, true
}
