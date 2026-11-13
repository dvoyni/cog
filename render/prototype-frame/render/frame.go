package render

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Window stands in for platform.Window.
type Window uint32

// Order places a command list among the frame's others, in one space every
// user of render shares. render's own loads and mips run below every user's.
type Order int32

// Color is a linear RGBA colour.
type Color struct{ R, G, B, A float64 }

// Rectangle is a viewport or scissor rectangle in target pixels.
type Rectangle struct{ X, Y, Width, Height uint32 }

// TextureDescriptor describes a temporary texture, abridged.
type TextureDescriptor struct {
	Label         string
	Width, Height uint32
	Format        string
	SampleCount   uint32
}

// ColorTarget is one colour target of a pass: a window, or a texture at one
// mip level and layer. Zero Clear keeps what the target holds; zero Discard
// stores what the pass draws.
type ColorTarget struct {
	Window     Window
	Texture    Texture
	MipLevel   uint32
	Layer      uint32
	Clear      bool
	ClearColor Color
	Discard    bool
	Resolve    Texture
}

// DepthStencilTarget is a pass's depth-stencil target; a zero Texture is
// none.
type DepthStencilTarget struct {
	Texture    Texture
	Clear      bool
	ClearDepth float32
	ReadOnly   bool
}

// PassDescriptor is what a pass declares; its pass parameters are set right
// after it begins.
type PassDescriptor struct {
	Label        string
	ColorTargets []ColorTarget
	DepthStencil DepthStencilTarget
}

// InstanceBuffer is a vertex buffer read once per instance, with its layout.
type InstanceBuffer struct {
	Range  BufferRange
	Layout VertexLayout
}

// DrawArgs are one draw's counts. A zero InstanceCount draws one instance; a
// zero mesh draws VertexCount vertices from no vertex buffer.
type DrawArgs struct {
	InstanceCount   uint32
	FirstInstance   uint32
	InstanceBuffers []InstanceBuffer
	VertexCount     uint32
}

// Frame is one frame's recording, from render's frame event to the submit.
type Frame struct {
	number      int
	assets      *Assets
	mutex       sync.Mutex
	lists       []*CommandList
	temporaries int
	drawn       map[uint32]bool
}

// BeginFrame is the prototype's stand-in for render's frame event.
func BeginFrame(number int, assets *Assets) *Frame {
	return &Frame{number: number, assets: assets, drawn: map[uint32]bool{}}
}

// AcquireTemporaryTexture hands out a texture from render's pool, held until
// the frame ends. Contents are undefined.
func (f *Frame) AcquireTemporaryTexture(descriptor TextureDescriptor) Texture {
	f.mutex.Lock()
	f.temporaries++
	number := f.temporaries
	f.mutex.Unlock()
	return Texture{f.assets.make(fmt.Sprintf("temporary texture %s #%d %dx%d %s", descriptor.Label, number, descriptor.Width, descriptor.Height, descriptor.Format))}
}

// BeginCommandList starts a command list at order. Each is filled by one
// task; lists fill concurrently.
func (f *Frame) BeginCommandList(label string, order Order) *CommandList {
	list := &CommandList{frame: f, label: label, order: order}
	f.mutex.Lock()
	f.lists = append(f.lists, list)
	f.mutex.Unlock()
	return list
}

// Submit is the prototype's stand-in for render's frame ending: it prints
// the command lists in order, as gfx would submit their encoders.
func (f *Frame) Submit() {
	sort.SliceStable(f.lists, func(i, j int) bool { return f.lists[i].order < f.lists[j].order })
	fmt.Printf("frame %d\n", f.number)
	fmt.Println("  order -1 render's own: the frame's loads, uploads and mips")
	for index, list := range f.lists {
		if index > 0 && f.lists[index-1].order == list.order {
			fmt.Printf("  order %d %s: REFUSED, order taken twice (reported once)\n", list.order, list.label)
			continue
		}
		fmt.Printf("  order %d %s\n", list.order, list.label)
		for _, line := range list.lines {
			fmt.Println("    " + line)
		}
	}
	fmt.Println("  materials drawn, with the values they hold at the submit:")
	materials := make([]int, 0, len(f.drawn))
	for index := range f.drawn {
		materials = append(materials, int(index))
	}
	sort.Ints(materials)
	for _, index := range materials {
		fmt.Printf("    %s: %s\n", f.assets.name(uint32(index)), strings.Join(f.assets.values[uint32(index)], "; "))
	}
}

// CommandList is one task's run of passes, compute passes and copies, kept
// in the order they were recorded.
type CommandList struct {
	frame *Frame
	label string
	order Order
	lines []string
	ended bool
	arena uint64
}

// AcquireTemporaryBuffer copies data into this list's arena chunk and hands
// out its range, valid until the frame ends.
func (l *CommandList) AcquireTemporaryBuffer(data []byte) BufferRange {
	offset := l.arena
	l.arena += (uint64(len(data)) + 255) &^ 255
	return BufferRange{Offset: offset, Size: uint64(len(data))}
}

// BeginPass starts a pass into the targets descriptor declares.
func (l *CommandList) BeginPass(descriptor PassDescriptor) *Pass {
	targets := make([]string, 0, len(descriptor.ColorTargets)+1)
	for _, target := range descriptor.ColorTargets {
		name := l.frame.assets.name(target.Texture.index)
		if target.Window != 0 {
			name = fmt.Sprintf("window %d", target.Window)
		}
		if target.Clear {
			name += fmt.Sprintf(" clear %v", target.ClearColor)
		}
		targets = append(targets, name)
	}
	if descriptor.DepthStencil.Texture != (Texture{}) {
		targets = append(targets, "depth "+l.frame.assets.name(descriptor.DepthStencil.Texture.index))
	}
	l.lines = append(l.lines, fmt.Sprintf("pass %q into %s", descriptor.Label, strings.Join(targets, ", ")))
	return &Pass{list: l}
}

// BeginComputePass starts a run of dispatches.
func (l *CommandList) BeginComputePass(label string) *ComputePass {
	l.lines = append(l.lines, fmt.Sprintf("compute pass %q", label))
	return &ComputePass{list: l}
}

// CopyBuffer copies source into destination at this point of the list.
func (l *CommandList) CopyBuffer(source, destination BufferRange) {
	l.lines = append(l.lines, fmt.Sprintf("copy %d bytes into %s at %d", source.Size, l.frame.assets.name(destination.Buffer.index), destination.Offset))
}

// End ends the list; render turns it into one gfx frame encoder.
func (l *CommandList) End() { l.ended = true }

// Pass records draws into its targets. Its parameters come in three levels:
// pass parameters (@group(0)), the material's (@group(1)), and draw
// parameters (@group(2)), which reset when the material changes.
type Pass struct {
	list     *CommandList
	material Material
	draws    int
}

// SetPassTexture binds a pass parameter, read by every material in the pass.
func (p *Pass) SetPassTexture(name string, texture Texture) {
	p.line("pass parameter %s = %s", name, p.list.frame.assets.name(texture.index))
}

// SetPassUniform binds a pass parameter's uniform block.
func (p *Pass) SetPassUniform[T any](name string, value T) {
	p.line("pass parameter %s = %+v", name, value)
}

// SetPassBuffer binds a pass parameter's storage buffer.
func (p *Pass) SetPassBuffer(name string, buffer BufferRange) {
	p.line("pass parameter %s = %d bytes at %d", name, buffer.Size, buffer.Offset)
}

// SetMaterial selects the material for the draws after it and resets every
// draw parameter.
func (p *Pass) SetMaterial(material Material) {
	p.material = material
	p.list.frame.mutex.Lock()
	p.list.frame.drawn[material.index] = true
	p.list.frame.mutex.Unlock()
	p.line("  %s; draw parameters reset", p.list.frame.assets.name(material.index))
}

// SetTexture binds a draw parameter for the draws after it.
func (p *Pass) SetTexture(name string, texture Texture) {
	p.line("    draw parameter %s = %s", name, p.list.frame.assets.name(texture.index))
}

// SetUniform binds a draw parameter's uniform block, copied into the arena
// and bound at a dynamic offset.
func (p *Pass) SetUniform[T any](name string, value T) {
	p.line("    draw parameter %s = %+v", name, value)
}

// SetBuffer binds a draw parameter's storage buffer.
func (p *Pass) SetBuffer(name string, buffer BufferRange) {
	p.line("    draw parameter %s = %d bytes at %d", name, buffer.Size, buffer.Offset)
}

// SetScissor changes the scissor rectangle for the draws after it.
func (p *Pass) SetScissor(rectangle Rectangle) {
	p.line("    scissor %+v", rectangle)
}

// Draw draws mesh with the current material and parameters.
func (p *Pass) Draw(mesh Mesh, arguments DrawArgs) {
	instances := max(arguments.InstanceCount, 1)
	what := p.list.frame.assets.name(mesh.index)
	if mesh == (Mesh{}) {
		what = fmt.Sprintf("%d vertices, no mesh", arguments.VertexCount)
	}
	p.draws++
	p.line("    draw %s x%d from instance %d, %d instance buffers", what, instances, arguments.FirstInstance, len(arguments.InstanceBuffers))
}

// End ends the pass.
func (p *Pass) End() { p.line("end pass (%d draws)", p.draws) }

func (p *Pass) line(format string, arguments ...any) {
	p.list.lines = append(p.list.lines, "  "+fmt.Sprintf(format, arguments...))
}

// ComputePass records dispatches. A dispatch's parameters are set by name in
// any group, with no levels; setting the shader resets them.
type ComputePass struct{ list *CommandList }

// SetShader selects the compute shader and resets every parameter.
func (c *ComputePass) SetShader(shader Shader) {
	c.line("%s; parameters reset", c.list.frame.assets.name(shader.index))
}

// SetBuffer binds a storage buffer by name.
func (c *ComputePass) SetBuffer(name string, buffer BufferRange) {
	c.line("  parameter %s = %d bytes at %d", name, buffer.Size, buffer.Offset)
}

// SetUniform binds a uniform block by name.
func (c *ComputePass) SetUniform[T any](name string, value T) {
	c.line("  parameter %s = %+v", name, value)
}

// Dispatch runs the shader over x by y by z workgroups.
func (c *ComputePass) Dispatch(x, y, z uint32) { c.line("  dispatch %dx%dx%d", x, y, z) }

// End ends the compute pass.
func (c *ComputePass) End() { c.line("end compute pass") }

func (c *ComputePass) line(format string, arguments ...any) {
	c.list.lines = append(c.list.lines, "  "+fmt.Sprintf(format, arguments...))
}
