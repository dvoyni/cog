package render

import (
	"fmt"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// Inventory is the task parameter a task reaches render's assets through, to
// acquire, make, write and release them, declared in its signature as kernel.Read is:
//
//	func loadLevel(k *kernel.Kernel, level Level, inventory render.Inventory) kernel.NoResult
//
// It locks render's asset table, gfx's Device and platform's FS, all for
// read, so tasks holding it run together and beside recording tasks, and its
// user names neither gfx nor its locks. A task that records and acquires
// declares it beside Frame. It is valid only while the task's job runs.
type Inventory struct {
	table  kernel.Read[*assetTable]
	device kernel.Read[gfx.Device]
	files  kernel.Read[platform.FS]
}

// Declare declares Inventory's locks and answers it bound to what they guard.
func (Inventory) Declare(access *kernel.Access) Inventory {
	return Inventory{table: access.Read[*assetTable](), device: access.Read[gfx.Device](), files: access.Read[platform.FS]()}
}

// AcquireTexture hands out the texture of path in platform's FS with
// options, counting one more holder of it: every acquire of the same path
// and options shares one texture until the last of them is released. The
// texture exists when the call returns, made from the file's header alone,
// with its size, the format of its colour space, 8 bits per channel, and
// with Mipmaps every mip level; render's jobs write its body before gfx
// submits the frame, whether it was acquired before the frame or while it
// records, so a draw recorded after the acquire reads it, and with Mipmaps
// draws every level below the first from it in render's own command list,
// which gfx submits before every user's. One acquired by a task the frame
// does not wait for, after its recording has ended, is written, and its mips
// drawn, before the next frame's submit instead. A file render
// cannot read or decode is reported once, and draws magenta: one texel when
// its header fails, its own size when its body does. A texture gfx refuses
// to make is logged, and the zero Texture answered. PNG and JPEG files load.
func (i Inventory) AcquireTexture(path string, options TextureOptions) Texture {
	table := i.table.Get()
	return table.loadedTextures.acquire(table, i.device.Get(), i.files.Get(), textureKey{path: path, options: options})
}

// AcquireBuffer hands out the buffer of path in platform's FS with usage,
// counting one more holder of it: every acquire of the same path and usage
// shares one buffer until the last of them is released, and one of another
// usage gets a buffer of its own. The buffer exists when the call returns,
// of the file's size as fs.Stat answers it, rounded up to a multiple of 4,
// with usage and the copy destination render's writes need; gfx refuses any
// other use of it. render's jobs write the
// file's bytes into it, zeros after them, before gfx submits the frame, at
// the points AcquireTexture's bodies are written at, so a draw recorded
// after the acquire reads them. A file fs.Stat fails on, a folder or a file
// of no bytes is reported once, and gets a buffer of no bytes; one that
// cannot be read when its bytes are written, or whose size changed since the
// acquire, is reported once, and its buffer keeps zeros. A buffer gfx
// refuses to make is logged, and the zero Buffer answered.
func (i Inventory) AcquireBuffer(path string, usage BufferUsageFlags) Buffer {
	table := i.table.Get()
	return table.loadedBuffers.acquire(table, i.device.Get(), i.files.Get(), bufferKey{path: path, usage: usage})
}

// AcquireShader hands out the variant of the shader at path in platform's FS
// with defines, counting one more holder of it: every acquire of the same
// path and defines, in any order, shares one variant until the last of them
// is released. It is compiled when the call returns, in the caller's job:
// read and preprocessed with defines, sorted by name, made a gfx module, and
// given a layout for each group up to the highest its bindings declare, of
// every binding it declares, used or not, an unused group's empty. Uniform
// blocks of @group(0) and @group(2), and storage buffers of @group(2), bind
// at dynamic offsets, and every binding is seen by each stage that may see
// it. Identical layouts are shared between shaders, so shaders sharing an
// include share its group's layout. Compiling is slow: a variant acquired
// first while a frame records stalls that recording. A file that fails to
// preprocess or compile is reported once, and one declaring @group(3), which
// render reserves, or past the device's limits, such as more than 8 uniform
// blocks or 4 storage buffers at dynamic offsets, or 8 storage buffers a
// stage sees, is refused and reported once; either answers a shader whose
// draws are skipped.
func (i Inventory) AcquireShader(path string, defines ...ShaderDefine) Shader {
	table := i.table.Get()
	key, sortedDefines := makeShaderKey(path, defines)
	return table.loadedShaders.acquire(table, i.device.Get(), i.files.Get(), key, sortedDefines)
}

// MakeBuffer makes a buffer filled from data, labelled label, with usage,
// which exists, filled, when the call returns: the bytes of data's elements
// as Go lays them out, padding included, so T is plain data with no
// pointers, laid out as the shader reads it, such as a number, or an array
// or struct of them. Its size is data's, rounded up to a multiple of 4 with
// zeros, and it carries usage and the copy destination, as AcquireBuffer's
// does. Its maker gives it back once, with ReleaseBuffer. A buffer of no
// bytes gets no gfx buffer, so nothing writes or binds it. A buffer gfx
// refuses to make is logged, and the zero Buffer answered.
func (i Inventory) MakeBuffer[T any](label string, usage BufferUsageFlags, data []T) Buffer {
	table := i.table.Get()
	device := i.device.Get()
	bytes := castToBytes(data)
	buffer, gfxBuffer, err := table.addBuffer(device, label, uint64(len(bytes)), usage)
	if err != nil {
		table.logUnlessLost(fmt.Errorf("render: making buffer %q: %w", label, err))
		return Buffer{}
	}
	table.madeBuffers.mutex.Lock()
	table.madeBuffers.add(buffer, buffer)
	table.madeBuffers.mutex.Unlock()
	if len(bytes) > 0 {
		writeBytes(table, device, gfxBuffer, 0, padBytes(bytes))
	}
	return buffer
}

// MakeTexture makes a texture of descriptor, whose zero counts mean one,
// which exists when the call returns, with every usage its format allows, as
// a temporary texture has: a pass target kept across frames, sampled, bound
// for storage where its format allows, and copied from and to. data fills its
// mip level 0, layer after layer, each layer's rows of texels packed tight,
// as WriteTexture lays out a region, T as MakeBuffer's; no data leaves it
// zero, and data of another length is refused and reported, leaving it zero.
// Levels below the first stay zero until written. Its maker gives it back
// once, with ReleaseTexture. A texture gfx refuses to make is logged, and the
// zero Texture answered.
func (i Inventory) MakeTexture[T any](descriptor TextureDescriptor, data []T) Texture {
	table := i.table.Get()
	device := i.device.Get()
	descriptor = descriptor.fillDefaults()
	texture, err := table.addTexture(device, descriptor)
	if err != nil {
		table.logUnlessLost(fmt.Errorf("render: making texture %q: %w", descriptor.Label, err))
		return Texture{}
	}
	table.madeTextures.mutex.Lock()
	table.madeTextures.add(texture, texture)
	table.madeTextures.mutex.Unlock()
	texels := castToBytes(data)
	if len(texels) == 0 {
		return texture
	}
	texelSize := descriptor.Format.info().texelSize
	levelSize := uint64(descriptor.Width) * uint64(descriptor.Height) * uint64(texelSize) * uint64(descriptor.LayerCount)
	if levelSize == 0 || descriptor.SampleCount > 1 || uint64(len(texels)) != levelSize {
		table.report(ReportKindWriteRefused, fmt.Errorf("render: making texture %q of %d samples from %d bytes, where it has one sample and its mip level 0 of %d layers holds %d", descriptor.Label, descriptor.SampleCount, len(texels), descriptor.LayerCount, levelSize))
		return texture
	}
	gfxTexture, _ := table.findTexture(texture)
	writeTexels(table, device, gfx.ImageCopyTexture{Texture: gfxTexture, Aspect: gfx.TextureAspectAll}, texels, texelSize,
		gfx.Extent3D{Width: descriptor.Width, Height: descriptor.Height, DepthOrArrayLayers: descriptor.LayerCount})
	return texture
}

// MakeMesh makes a mesh of descriptor: its vertex buffers, ranges of buffers
// render holds with their layouts, its index buffer, and its topology. A
// draw of it covers all of it: as many vertices as the fewest its vertex
// ranges hold, by their strides, or, with an index buffer, as many indices
// as its range holds. The mesh owns none of the buffers it names: a draw of
// it after one of them is freed, or one that failed, is skipped and
// reported once. Its maker gives it back once, with ReleaseMesh. A mesh is
// refused and reported, answering the zero Mesh, for more vertex buffers
// than the device allows (8 by WebGPU's default limits), a vertex buffer at
// an offset or of a stride no multiple of 4, an attribute at an offset no
// multiple of 4, or of 2 for a format of two 8-bit components, an index
// buffer of no index format or at an offset no multiple of its index's size,
// or a topology gfx has none of.
// Once render's tables have grown, a mesh of vertex layouts drawn before
// allocates nothing.
func (i Inventory) MakeMesh(descriptor MeshDescriptor) Mesh {
	table := i.table.Get()
	mesh, err := table.addMesh(i.device.Get(), descriptor)
	if err != nil {
		table.report(ReportKindMeshRefused, fmt.Errorf("render: making mesh %q: %w", descriptor.Label, err))
		return Mesh{}
	}
	table.madeMeshes.mutex.Lock()
	table.madeMeshes.add(mesh, mesh)
	table.madeMeshes.mutex.Unlock()
	return mesh
}

// MakeMaterial makes a material of descriptor: its shader, render state and
// entry points. A material owns nothing it names: a draw of it after its
// shader is freed, or of one whose shader failed, is skipped and reported
// once. An empty entry point names the shader's only function of its stage,
// and a shader with no @fragment function makes a material with no fragment
// stage, drawn only into passes with no colour target, such as a shadow map
// or a depth prepass; a material with a fragment function drawn into such a
// pass runs it with no targets, so alpha-tested shadows still discard. A
// material naming a function its shader lacks, or none of a stage where its
// shader has several, is refused and reported once, and its draws are
// skipped. Its maker gives it back once, with ReleaseMaterial.
func (i Inventory) MakeMaterial(descriptor MaterialDescriptor) Material {
	table := i.table.Get()
	material, err := table.addMaterial(descriptor)
	if err != nil {
		table.report(ReportKindMaterialRefused, fmt.Errorf("render: making material %q of shader %v: %w", descriptor.Label, descriptor.Shader, err))
	}
	table.madeMaterials.mutex.Lock()
	table.madeMaterials.add(material, material)
	table.madeMaterials.mutex.Unlock()
	return material
}

// WriteBuffer writes data into buffer at offset, T as MakeBuffer's: a
// loaded, made or other buffer render holds. The write lands before every
// command list of gfx's next submit, so it fills the buffer for the whole
// frame being recorded, or, once that frame is flushed, the next one; two
// writes to one region in a frame leave the last. To change data between
// passes, write a temporary buffer and copy it at that point in a command
// list. A write that does not fit is refused and reported: into a buffer
// render does not hold or of no bytes, at an offset or of a length that is
// not a multiple of 4, or ending past the buffer.
func (i Inventory) WriteBuffer[T any](buffer Buffer, offset uint64, data []T) {
	i.table.Get().writeBuffer(i.device.Get(), buffer, offset, castToBytes(data))
}

// WriteTexture writes data into region of texture, one mip level and one
// layer, so an atlas page lands in a layer of a 2D-array texture: rows of
// texels packed tight, each texel of its format's size, T as MakeBuffer's.
// It lands, as WriteBuffer's does, before every command list of gfx's next
// submit, two writes to one region in a frame leaving the last. A write that
// does not fit is refused and reported: into a texture render does not
// hold, of several samples, or of a depth or depth-stencil format beyond
// Depth16Unorm, which writes cannot fill; at a mip level or layer it lacks;
// into a region past its level's edges; or of data not as long as the
// region.
func (i Inventory) WriteTexture[T any](texture Texture, region TextureRegion, data []T) {
	i.table.Get().writeTexture(i.device.Get(), texture, region, castToBytes(data))
}

// ReleaseTexture gives back one acquire of texture, or the make of a made
// one. When a loaded texture's last acquire is given back, or a made texture
// is, the texture is freed at the end of that frame, after gfx's present, so
// the frame's draws still read it; an acquire of a loaded texture's path and
// options before then holds it again. A texture no acquire or make holds,
// such as a temporary, one released as often as it was acquired, a made one
// released already, or the zero Texture, is refused and reported.
func (i Inventory) ReleaseTexture(texture Texture) {
	table := i.table.Get()
	if !table.loadedTextures.release(texture) && !table.madeTextures.release(texture) {
		table.report(ReportKindReleaseRefused, fmt.Errorf("render: releasing texture %v, which no acquire or make holds", texture))
	}
}

// ReleaseBuffer gives back one acquire of buffer, or the make of a made one,
// freeing the buffer as ReleaseTexture frees a texture: at the end of that
// frame, after gfx's present. A buffer no acquire or make holds, such as one
// released as often as it was acquired, a made one released already, or the
// zero Buffer, is refused and reported.
func (i Inventory) ReleaseBuffer(buffer Buffer) {
	table := i.table.Get()
	if !table.loadedBuffers.release(buffer) && !table.madeBuffers.release(buffer) {
		table.report(ReportKindReleaseRefused, fmt.Errorf("render: releasing buffer %v, which no acquire or make holds", buffer))
	}
}

// ReleaseShader gives back one acquire of shader, freeing the shader as
// ReleaseTexture frees a texture: at the end of that frame, after gfx's
// present, with the layouts no other shader shares. A shader no acquire
// holds, such as one released as often as it was acquired, or the zero
// Shader, is refused and reported.
func (i Inventory) ReleaseShader(shader Shader) {
	table := i.table.Get()
	if !table.loadedShaders.release(shader) {
		table.report(ReportKindReleaseRefused, fmt.Errorf("render: releasing shader %v, which no acquire holds", shader))
	}
}

// ReleaseMesh gives back the make of mesh, freeing it at the end of that
// frame, after gfx's present, so the frame's draws still draw it. A mesh no
// make holds, such as one released already or the zero Mesh, is refused and
// reported.
func (i Inventory) ReleaseMesh(mesh Mesh) {
	table := i.table.Get()
	if !table.madeMeshes.release(mesh) {
		table.report(ReportKindReleaseRefused, fmt.Errorf("render: releasing mesh %v, which no make holds", mesh))
	}
}

// ReleaseMaterial gives back the make of material, freeing it as ReleaseMesh
// frees a mesh: at the end of that frame, after gfx's present. A material no
// make holds, such as one released already or the zero Material, is refused
// and reported.
func (i Inventory) ReleaseMaterial(material Material) {
	table := i.table.Get()
	if !table.madeMaterials.release(material) {
		table.report(ReportKindReleaseRefused, fmt.Errorf("render: releasing material %v, which no make holds", material))
	}
}

// castToBytes answers the bytes data's elements are laid out in, in data's
// own array.
func castToBytes[T any](data []T) []byte {
	if len(data) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(data))), len(data)*int(unsafe.Sizeof(data[0])))
}
