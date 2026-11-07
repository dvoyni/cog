package gfx

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
)

// recoveryLogKey is the key a failed recovery is logged once under, until a
// recovery succeeds.
const recoveryLogKey = "gfx: recovering the lost device"

// device is the Device resource's value: installed empty in Init and filled
// in Start, before any job reads it. It has no exclusive operation, so a job
// holding the read lock cannot assert it into an ExclusiveDevice.
type device struct {
	instance InstanceSocket
	// adapter and backend are what Start or the last recovery requested, nil
	// where a request failed; adapterOptions and deviceDescriptor are what
	// Start requested them with, and a recovery requests them with again.
	adapter          BackendAdapter
	adapterOptions   RequestAdapterOptions
	deviceDescriptor DeviceDescriptor
	backend          BackendDevice
	features         FeatureFlags
	limits           Limits
	adapterInfo      AdapterInfo
	// kernel is gfx's: BufferMappedEvent is published on it, and what a call
	// answers no error for goes to its logger.
	kernel *kernel.Kernel
	// lost is set by the first backend call found answering ErrDeviceLost:
	// from then on, until a recovery, gfx makes no backend call but the
	// recovery's, its own calls with errors answer ErrDeviceLost, and its
	// releases release no object, which the lost device's Release does.
	lost atomic.Bool
	// nextRecoveryAt is the earliest Beat.TimeFromStart a recovery is tried
	// at, a second after the last that failed; only the recovery reads and
	// changes it, under the write lock.
	nextRecoveryAt time.Duration
	// surfaces are the windows with a surface, in the order they got it;
	// only jobs holding the write lock change it.
	surfaces []platform.Window
	// surfaceRequest is what every surface is configured from.
	surfaceRequest SurfaceRequest
	// configurations are what each window's surface is configured with;
	// only jobs holding the write lock change them.
	configurations map[platform.Window]SurfaceConfiguration
	frames         framePool
	// acquireStopwatch times each backend acquire, and submissionStopwatch,
	// running since Init, stamps the frames' submits and the polls that see
	// them finished: the frame timing signals, by metronome's clock.
	acquireStopwatch    metronome.Stopwatch
	submissionStopwatch metronome.Stopwatch
	// mapMutex guards pendingMaps, the buffers whose maps MapBuffer or a
	// capture started and no poll has found resolved, and is held across the
	// poll's checks, so two polls never publish one map twice. It guards the
	// captures too.
	mapMutex    sync.Mutex
	pendingMaps []Buffer
	// capturingWindows are the windows with a capture in flight, from
	// RequestCapture until its CapturedEvent is published.
	capturingWindows []platform.Window
	// requestedCaptures are the windows whose captures no flush has taken.
	requestedCaptures []platform.Window
	// pendingCaptures are the captures whose buffers' maps are pending,
	// published once the poll finds them resolved.
	pendingCaptures []CapturedEvent
	// Each table holds its type's handles with what each was created with,
	// slices copied, which a recovery creates it again from.
	buffers          table[Buffer, BufferDescriptor]
	textures         table[Texture, TextureDescriptor]
	textureViews     table[TextureView, textureViewSource]
	samplers         table[Sampler, SamplerDescriptor]
	shaderModules    table[ShaderModule, ShaderModuleDescriptor]
	bindGroupLayouts table[BindGroupLayout, BindGroupLayoutDescriptor]
	bindGroups       table[BindGroup, BindGroupDescriptor]
	pipelineLayouts  table[PipelineLayout, PipelineLayoutDescriptor]
	renderPipelines  table[RenderPipeline, pipelineSource[RenderPipelineDescriptor]]
	computePipelines table[ComputePipeline, pipelineSource[ComputePipelineDescriptor]]
}

func (d *device) Features() FeatureFlags { return d.features }

func (d *device) Limits() Limits { return d.limits }

func (d *device) AdapterInfo() AdapterInfo { return d.adapterInfo }

func (d *device) CreateBuffer(descriptor BufferDescriptor) (Buffer, error) {
	if d.lost.Load() {
		return Buffer{}, ErrDeviceLost
	}
	buffer, err := createResource(&d.buffers, descriptor, d.backend.CreateBuffer)
	return buffer, d.checkLoss(err)
}

func (d *device) ReleaseBuffer(buffer Buffer) {
	releaseResource(d, &d.buffers, buffer, BackendDevice.ReleaseBuffer)
}

func (d *device) BufferDescriptor(buffer Buffer) BufferDescriptor {
	descriptor, _ := d.buffers.descriptor(buffer)
	return descriptor
}

func (d *device) CreateTexture(descriptor TextureDescriptor) (Texture, error) {
	if d.lost.Load() {
		return Texture{}, ErrDeviceLost
	}
	descriptor.ViewFormats = slices.Clone(descriptor.ViewFormats)
	texture, err := createResource(&d.textures, descriptor, d.backend.CreateTexture)
	return texture, d.checkLoss(err)
}

func (d *device) ReleaseTexture(texture Texture) {
	releaseResource(d, &d.textures, texture, BackendDevice.ReleaseTexture)
}

func (d *device) TextureDescriptor(texture Texture) TextureDescriptor {
	descriptor, _ := d.textures.descriptor(texture)
	descriptor.ViewFormats = slices.Clone(descriptor.ViewFormats)
	return descriptor
}

func (d *device) CreateTextureView(texture Texture, descriptor TextureViewDescriptor) (TextureView, error) {
	if d.lost.Load() {
		return TextureView{}, ErrDeviceLost
	}
	if err := d.textures.check(texture); err != nil {
		return TextureView{}, fmt.Errorf("%w: the view's texture", err)
	}
	source := textureViewSource{texture: texture, descriptor: descriptor}
	view, err := createResource(&d.textureViews, source, func(view TextureView, source textureViewSource) error {
		return d.backend.CreateTextureView(view, source.texture, source.descriptor)
	})
	return view, d.checkLoss(err)
}

func (d *device) ReleaseTextureView(view TextureView) {
	releaseResource(d, &d.textureViews, view, BackendDevice.ReleaseTextureView)
}

func (d *device) TextureViewTexture(view TextureView) Texture {
	source, _ := d.textureViews.descriptor(view)
	return source.texture
}

func (d *device) CreateSampler(descriptor SamplerDescriptor) (Sampler, error) {
	if d.lost.Load() {
		return Sampler{}, ErrDeviceLost
	}
	sampler, err := createResource(&d.samplers, descriptor, d.backend.CreateSampler)
	return sampler, d.checkLoss(err)
}

func (d *device) ReleaseSampler(sampler Sampler) {
	releaseResource(d, &d.samplers, sampler, BackendDevice.ReleaseSampler)
}

func (d *device) CreateQuerySet(descriptor QuerySetDescriptor) (QuerySet, error) {
	panic("gfx listing: no bodies")
}

func (d *device) ReleaseQuerySet(querySet QuerySet) { panic("gfx listing: no bodies") }

func (d *device) TimestampPeriod() float32 { panic("gfx listing: no bodies") }

func (d *device) CreateShaderModule(descriptor ShaderModuleDescriptor) (ShaderModule, error) {
	if d.lost.Load() {
		return ShaderModule{}, ErrDeviceLost
	}
	module, err := createResource(&d.shaderModules, descriptor, d.backend.CreateShaderModule)
	return module, d.checkLoss(err)
}

func (d *device) ReleaseShaderModule(module ShaderModule) {
	releaseResource(d, &d.shaderModules, module, BackendDevice.ReleaseShaderModule)
}

func (d *device) CreateBindGroupLayout(descriptor BindGroupLayoutDescriptor) (BindGroupLayout, error) {
	if d.lost.Load() {
		return BindGroupLayout{}, ErrDeviceLost
	}
	descriptor.Entries = slices.Clone(descriptor.Entries)
	layout, err := createResource(&d.bindGroupLayouts, descriptor, d.backend.CreateBindGroupLayout)
	return layout, d.checkLoss(err)
}

func (d *device) ReleaseBindGroupLayout(layout BindGroupLayout) {
	releaseResource(d, &d.bindGroupLayouts, layout, BackendDevice.ReleaseBindGroupLayout)
}

func (d *device) CreateBindGroup(descriptor BindGroupDescriptor) (BindGroup, error) {
	if d.lost.Load() {
		return BindGroup{}, ErrDeviceLost
	}
	if err := d.bindGroupLayouts.check(descriptor.Layout); err != nil {
		return BindGroup{}, fmt.Errorf("%w: the bind group's layout", err)
	}
	for _, entry := range descriptor.Entries {
		if entry.Buffer != (Buffer{}) && d.buffers.check(entry.Buffer) != nil {
			return BindGroup{}, fmt.Errorf("%w: the buffer at binding %d", ErrReleased, entry.Binding)
		}
		if entry.Sampler != (Sampler{}) && d.samplers.check(entry.Sampler) != nil {
			return BindGroup{}, fmt.Errorf("%w: the sampler at binding %d", ErrReleased, entry.Binding)
		}
		if entry.TextureView != (TextureView{}) && d.textureViews.check(entry.TextureView) != nil {
			return BindGroup{}, fmt.Errorf("%w: the texture view at binding %d", ErrReleased, entry.Binding)
		}
	}
	descriptor.Entries = slices.Clone(descriptor.Entries)
	group, err := createResource(&d.bindGroups, descriptor, d.backend.CreateBindGroup)
	return group, d.checkLoss(err)
}

func (d *device) ReleaseBindGroup(group BindGroup) {
	releaseResource(d, &d.bindGroups, group, BackendDevice.ReleaseBindGroup)
}

func (d *device) CreatePipelineLayout(descriptor PipelineLayoutDescriptor) (PipelineLayout, error) {
	if d.lost.Load() {
		return PipelineLayout{}, ErrDeviceLost
	}
	for group, layout := range descriptor.BindGroupLayouts {
		if err := d.bindGroupLayouts.check(layout); err != nil {
			return PipelineLayout{}, fmt.Errorf("%w: the layout of group %d", err, group)
		}
	}
	descriptor.BindGroupLayouts = slices.Clone(descriptor.BindGroupLayouts)
	layout, err := createResource(&d.pipelineLayouts, descriptor, d.backend.CreatePipelineLayout)
	return layout, d.checkLoss(err)
}

func (d *device) ReleasePipelineLayout(layout PipelineLayout) {
	releaseResource(d, &d.pipelineLayouts, layout, BackendDevice.ReleasePipelineLayout)
}

func (d *device) CreateRenderPipeline(descriptor RenderPipelineDescriptor) (RenderPipeline, error) {
	if d.lost.Load() {
		return RenderPipeline{}, ErrDeviceLost
	}
	if descriptor.Layout != (PipelineLayout{}) {
		if err := d.pipelineLayouts.check(descriptor.Layout); err != nil {
			return RenderPipeline{}, fmt.Errorf("%w: the pipeline's layout", err)
		}
	}
	if err := d.shaderModules.check(descriptor.Vertex.Module); err != nil {
		return RenderPipeline{}, fmt.Errorf("%w: the vertex module", err)
	}
	if descriptor.Fragment.Module != (ShaderModule{}) && d.shaderModules.check(descriptor.Fragment.Module) != nil {
		return RenderPipeline{}, fmt.Errorf("%w: the fragment module", ErrReleased)
	}
	inputs, err := d.backend.ReflectVertexInputs(descriptor.Vertex.Module, descriptor.Vertex.EntryPoint)
	if err != nil {
		return RenderPipeline{}, d.checkLoss(err)
	}
	if err := checkVertexInputs(descriptor.Vertex, inputs); err != nil {
		return RenderPipeline{}, err
	}
	source := pipelineSource[RenderPipelineDescriptor]{descriptor: descriptor.clone()}
	autoLayout := source.descriptor.Layout == (PipelineLayout{})
	groups := d.groupDescriptors(source.descriptor.Layout)
	if autoLayout {
		if groups, err = d.backend.ReflectRenderPipelineLayout(source.descriptor); err != nil {
			return RenderPipeline{}, d.checkLoss(err)
		}
	}
	if err := d.limits.checkRenderPipeline(groups, source.descriptor.Vertex.Buffers); err != nil {
		return RenderPipeline{}, err
	}
	if autoLayout {
		if source.descriptor.Layout, err = d.createAutoLayout(groups); err != nil {
			return RenderPipeline{}, err
		}
		source.autoLayout = true
	}
	pipeline, err := createResource(&d.renderPipelines, source, func(pipeline RenderPipeline, source pipelineSource[RenderPipelineDescriptor]) error {
		return d.backend.CreateRenderPipeline(pipeline, source.descriptor)
	})
	if err != nil && source.autoLayout {
		d.releaseAutoLayout(source.descriptor.Layout)
	}
	return pipeline, d.checkLoss(err)
}

func (d *device) ReleaseRenderPipeline(pipeline RenderPipeline) {
	source, _ := d.renderPipelines.descriptor(pipeline)
	if releaseResource(d, &d.renderPipelines, pipeline, BackendDevice.ReleaseRenderPipeline) && source.autoLayout {
		d.releaseAutoLayout(source.descriptor.Layout)
	}
}

func (d *device) RenderPipelineBindGroupLayout(pipeline RenderPipeline, group uint32) BindGroupLayout {
	source, _ := d.renderPipelines.descriptor(pipeline)
	return d.groupLayout(source.descriptor.Layout, group)
}

func (d *device) CreateComputePipeline(descriptor ComputePipelineDescriptor) (ComputePipeline, error) {
	if d.lost.Load() {
		return ComputePipeline{}, ErrDeviceLost
	}
	if descriptor.Layout != (PipelineLayout{}) {
		if err := d.pipelineLayouts.check(descriptor.Layout); err != nil {
			return ComputePipeline{}, fmt.Errorf("%w: the pipeline's layout", err)
		}
	}
	if err := d.shaderModules.check(descriptor.Module); err != nil {
		return ComputePipeline{}, fmt.Errorf("%w: the module", err)
	}
	descriptor.Constants = maps.Clone(descriptor.Constants)
	source := pipelineSource[ComputePipelineDescriptor]{descriptor: descriptor}
	autoLayout := source.descriptor.Layout == (PipelineLayout{})
	groups := d.groupDescriptors(source.descriptor.Layout)
	var err error
	if autoLayout {
		if groups, err = d.backend.ReflectComputePipelineLayout(source.descriptor); err != nil {
			return ComputePipeline{}, d.checkLoss(err)
		}
	}
	if err := d.limits.checkPipelineLayout(groups); err != nil {
		return ComputePipeline{}, err
	}
	if autoLayout {
		if source.descriptor.Layout, err = d.createAutoLayout(groups); err != nil {
			return ComputePipeline{}, err
		}
		source.autoLayout = true
	}
	pipeline, err := createResource(&d.computePipelines, source, func(pipeline ComputePipeline, source pipelineSource[ComputePipelineDescriptor]) error {
		return d.backend.CreateComputePipeline(pipeline, source.descriptor)
	})
	if err != nil && source.autoLayout {
		d.releaseAutoLayout(source.descriptor.Layout)
	}
	return pipeline, d.checkLoss(err)
}

func (d *device) ReleaseComputePipeline(pipeline ComputePipeline) {
	source, _ := d.computePipelines.descriptor(pipeline)
	if releaseResource(d, &d.computePipelines, pipeline, BackendDevice.ReleaseComputePipeline) && source.autoLayout {
		d.releaseAutoLayout(source.descriptor.Layout)
	}
}

func (d *device) ComputePipelineBindGroupLayout(pipeline ComputePipeline, group uint32) BindGroupLayout {
	source, _ := d.computePipelines.descriptor(pipeline)
	return d.groupLayout(source.descriptor.Layout, group)
}

func (d *device) WriteBuffer(buffer Buffer, offset uint64, data []byte) error {
	if d.lost.Load() {
		return ErrDeviceLost
	}
	if err := d.buffers.check(buffer); err != nil {
		return err
	}
	return d.checkLoss(d.backend.WriteBuffer(buffer, offset, data))
}

func (d *device) WriteTexture(destination ImageCopyTexture, data []byte, layout ImageDataLayout, size Extent3D) error {
	if d.lost.Load() {
		return ErrDeviceLost
	}
	if err := d.textures.check(destination.Texture); err != nil {
		return err
	}
	return d.checkLoss(d.backend.WriteTexture(destination, data, layout, size))
}

func (d *device) Encoder(frame Frame, order SubmitOrder) (CommandEncoder, error) {
	if d.lost.Load() {
		return discardEncoder{}, ErrDeviceLost
	}
	encoder, err := d.backend.CreateCommandEncoder(CommandEncoderDescriptor{})
	if err != nil {
		return discardEncoder{}, d.checkLoss(err)
	}
	if err := d.frames.addEncoder(frame, frameEncoder{order: order, encoder: encoder}); err != nil {
		encoder.DiscardEncoding()
		return discardEncoder{}, err
	}
	return encoder, nil
}

func (d *device) Framebuffer(frame Frame, window platform.Window) TextureView {
	return d.frames.framebuffer(frame, window)
}

func (d *device) SurfaceConfiguration(window platform.Window) (SurfaceConfiguration, bool) {
	configuration, configured := d.configurations[window]
	return configuration, configured
}

func (d *device) CreateCommandEncoder(descriptor CommandEncoderDescriptor) (StandaloneCommandEncoder, error) {
	if d.lost.Load() {
		return nil, ErrDeviceLost
	}
	encoder, err := d.backend.CreateCommandEncoder(descriptor)
	return encoder, d.checkLoss(err)
}

// Submit submits buffers, then polls the maps, even when the submit fails.
func (d *device) Submit(buffers ...CommandBuffer) error {
	if d.lost.Load() {
		return ErrDeviceLost
	}
	_, err := d.backend.Submit(buffers...)
	err = d.checkLoss(err)
	d.Poll()
	return err
}

func (d *device) MapBuffer(buffer Buffer, mode MapMode, offset, size uint64) error {
	if d.lost.Load() {
		return ErrDeviceLost
	}
	if err := d.buffers.check(buffer); err != nil {
		return err
	}
	if err := d.backend.MapBuffer(buffer, mode, offset, size); err != nil {
		return d.checkLoss(err)
	}
	d.mapMutex.Lock()
	defer d.mapMutex.Unlock()
	d.pendingMaps = append(d.pendingMaps, buffer)
	return nil
}

func (d *device) MappedRange(buffer Buffer, offset, size uint64) ([]byte, error) {
	if d.lost.Load() {
		return nil, ErrDeviceLost
	}
	if err := d.buffers.check(buffer); err != nil {
		return nil, err
	}
	mapped, err := d.backend.MappedRange(buffer, offset, size)
	return mapped, d.checkLoss(err)
}

// UnmapBuffer ends buffer's map; one not yet resolved publishes nothing.
func (d *device) UnmapBuffer(buffer Buffer) error {
	if d.lost.Load() {
		return ErrDeviceLost
	}
	if err := d.buffers.check(buffer); err != nil {
		return err
	}
	d.dropPendingMap(buffer)
	return d.checkLoss(d.backend.UnmapBuffer(buffer))
}

// Poll has the backend resolve its finished maps, then publishes
// BufferMappedEvent, from this job, for each pending map now resolved:
// MapStatusMapped, or MapStatusFailed when the backend answers its status
// an error, which goes to LogErr first. A capture's map resolved publishes
// CapturedEvent instead, CaptureStatusFailed with its buffer released for
// one that failed. A pending map whose buffer was released is dropped and
// publishes nothing. A status answering a device loss asks no other, and
// the loss fails every map pending. It checks, too, which frames' submits
// the GPU has finished, for DrawEvent.GPULatency. A lost device polls
// nothing.
func (d *device) Poll() {
	if d.lost.Load() {
		return
	}
	d.frames.completeSubmissions(d.backend.Poll(), d.submissionStopwatch.Elapsed())
	d.mapMutex.Lock()
	var resolved []BufferMappedEvent
	var captures []CapturedEvent
	var failures []error
	var loss error
	d.pendingMaps = slices.DeleteFunc(d.pendingMaps, func(buffer Buffer) bool {
		if loss != nil {
			return false
		}
		if d.buffers.check(buffer) != nil {
			return true
		}
		ready, err := d.backend.MapStatus(buffer)
		if errors.Is(err, ErrDeviceLost) {
			loss = err
			return false
		}
		if err == nil && !ready {
			return false
		}
		if err != nil {
			failures = append(failures, err)
		}
		if index := slices.IndexFunc(d.pendingCaptures, func(capture CapturedEvent) bool { return capture.Buffer == buffer }); index >= 0 {
			captures = append(captures, d.pendingCaptures[index])
			d.pendingCaptures = slices.Delete(d.pendingCaptures, index, index+1)
			if err != nil {
				captures[len(captures)-1].Status = CaptureStatusFailed
			}
			return true
		}
		status := MapStatusMapped
		if err != nil {
			status = MapStatusFailed
		}
		resolved = append(resolved, BufferMappedEvent{Buffer: buffer, Status: status})
		return true
	})
	d.mapMutex.Unlock()
	for _, err := range failures {
		d.logErr(err)
	}
	d.checkLoss(loss)
	for _, event := range resolved {
		d.kernel.Publish(event)
	}
	for _, capture := range captures {
		if capture.Status == CaptureStatusFailed {
			d.failCapture(capture)
			continue
		}
		d.deliverCapture(capture)
	}
}

// RequestCapture takes window's capture for the next frame flushed, judged
// by what its surface is configured with: a window not yet configured
// answers ErrCaptureUnsupported too.
func (d *device) RequestCapture(window platform.Window) error {
	if d.lost.Load() {
		return ErrDeviceLost
	}
	configuration, configured := d.configurations[window]
	if !configured || !configuration.Capturable || !slices.Contains([]TextureFormat{
		TextureFormatBGRA8Unorm, TextureFormatBGRA8UnormSrgb, TextureFormatRGBA8Unorm, TextureFormatRGBA8UnormSrgb,
	}, configuration.Format) {
		return ErrCaptureUnsupported
	}
	d.mapMutex.Lock()
	defer d.mapMutex.Unlock()
	if slices.Contains(d.capturingWindows, window) {
		return ErrCaptureBusy
	}
	d.capturingWindows = append(d.capturingWindows, window)
	d.requestedCaptures = append(d.requestedCaptures, window)
	return nil
}

// checkLoss answers err. When err is a device loss and the device was not
// lost yet, it marks the device lost, logs err, publishes DeviceLostEvent
// and fails the maps pending, from this job: once, whichever job sees it
// first.
func (d *device) checkLoss(err error) error {
	if errors.Is(err, ErrDeviceLost) && d.lost.CompareAndSwap(false, true) {
		d.kernel.Logger().LogErr(err)
		d.kernel.Publish(DeviceLostEvent{})
		d.failPendingMaps()
	}
	return err
}

// logErr logs err, an error a backend call answered, through gfx's logger;
// a device loss goes through checkLoss, so it is logged once.
func (d *device) logErr(err error) {
	if errors.Is(err, ErrDeviceLost) {
		d.checkLoss(err)
		return
	}
	d.kernel.Logger().LogErr(err)
}

// createAutoLayout creates a bind-group layout of each of groups, as the
// backend reflected them for a pipeline's "auto" layout, and a pipeline
// layout of them. When a creation fails, it releases what it created and
// answers the error.
func (d *device) createAutoLayout(groups []BindGroupLayoutDescriptor) (PipelineLayout, error) {
	groupLayouts := make([]BindGroupLayout, 0, len(groups))
	for _, group := range groups {
		groupLayout, err := d.CreateBindGroupLayout(group)
		if err != nil {
			for _, created := range groupLayouts {
				d.ReleaseBindGroupLayout(created)
			}
			return PipelineLayout{}, err
		}
		groupLayouts = append(groupLayouts, groupLayout)
	}
	layout, err := d.CreatePipelineLayout(PipelineLayoutDescriptor{BindGroupLayouts: groupLayouts})
	if err != nil {
		for _, created := range groupLayouts {
			d.ReleaseBindGroupLayout(created)
		}
		return PipelineLayout{}, err
	}
	return layout, nil
}

// copyCaptures takes the captures requested and, for each window the frame
// in slot acquired, creates a staging buffer and records the copy of its
// framebuffer into it on gfx's own encoder, whose command buffer it appends
// to slot's, after every submit order. It answers the captures copied, for
// mapCaptures once submitted. A window the frame did not acquire is
// delivered CaptureStatusNotAcquired; a buffer, encoder or Finish that fails
// goes to LogErr, and its captures are delivered CaptureStatusFailed.
func (d *device) copyCaptures(slot *frameSlot) []CapturedEvent {
	d.mapMutex.Lock()
	requested := d.requestedCaptures
	d.requestedCaptures = nil
	d.mapMutex.Unlock()
	var copied []CapturedEvent
	var encoder StandaloneCommandEncoder
	for _, window := range requested {
		capture := CapturedEvent{Window: window, Beat: slot.beat, Status: CaptureStatusNotAcquired}
		index := slices.IndexFunc(slot.framebuffers, func(acquired framebuffer) bool { return acquired.window == window })
		if index < 0 {
			d.deliverCapture(capture)
			continue
		}
		texture := slot.framebuffers[index].texture
		descriptor, _ := d.textures.descriptor(texture)
		capture.Width = descriptor.Size.Width
		capture.Height = descriptor.Size.Height
		capture.Format = descriptor.Format
		capture.BytesPerRow = (capture.Width*4 + 255) &^ 255
		capture.Status = CaptureStatusCaptured
		var err error
		if encoder == nil {
			encoder, err = d.backend.CreateCommandEncoder(CommandEncoderDescriptor{Label: "gfx: captures"})
			err = d.checkLoss(err)
		}
		if err == nil {
			capture.Buffer, err = d.CreateBuffer(BufferDescriptor{
				Label: "gfx: capture",
				Size:  uint64(capture.BytesPerRow) * uint64(capture.Height),
				Usage: BufferUsageFlagCopyDst | BufferUsageFlagMapRead,
			})
		}
		if err != nil {
			d.logErr(fmt.Errorf("gfx: capturing %v: %w", window, err))
			d.failCapture(capture)
			continue
		}
		encoder.CopyTextureToBuffer(texture, capture.Buffer, []BufferTextureCopy{{
			BufferLayout: ImageDataLayout{BytesPerRow: capture.BytesPerRow, RowsPerImage: capture.Height},
			TextureBase:  ImageCopyTexture{Texture: texture},
			Size:         Extent3D{Width: capture.Width, Height: capture.Height, DepthOrArrayLayers: 1},
		}})
		copied = append(copied, capture)
	}
	if len(copied) == 0 {
		if encoder != nil {
			encoder.DiscardEncoding()
		}
		return nil
	}
	commandBuffer, err := encoder.Finish()
	if err != nil {
		d.logErr(fmt.Errorf("gfx: finishing the captures' encoder: %w", err))
		for _, capture := range copied {
			d.failCapture(capture)
		}
		return nil
	}
	slot.commandBuffers = append(slot.commandBuffers, commandBuffer)
	return copied
}

// mapCaptures starts the map of each copied capture's buffer, its capture
// pending until a poll finds it resolved. When the submit failed, which went
// to LogErr, or a map is refused, which goes there, the capture is delivered
// CaptureStatusFailed.
func (d *device) mapCaptures(copied []CapturedEvent, submitted bool) {
	for _, capture := range copied {
		if submitted {
			err := d.backend.MapBuffer(capture.Buffer, MapModeRead, 0, uint64(capture.BytesPerRow)*uint64(capture.Height))
			if err == nil {
				d.mapMutex.Lock()
				d.pendingMaps = append(d.pendingMaps, capture.Buffer)
				d.pendingCaptures = append(d.pendingCaptures, capture)
				d.mapMutex.Unlock()
				continue
			}
			d.logErr(fmt.Errorf("gfx: mapping the capture of %v: %w", capture.Window, err))
		}
		d.failCapture(capture)
	}
}

// failCapture releases capture's buffer, if it has one, and delivers it
// CaptureStatusFailed, with no buffer.
func (d *device) failCapture(capture CapturedEvent) {
	if capture.Buffer != (Buffer{}) {
		d.ReleaseBuffer(capture.Buffer)
	}
	capture.Buffer = Buffer{}
	capture.Status = CaptureStatusFailed
	d.deliverCapture(capture)
}

// deliverCapture ends the capture of capture's window, so it can be
// requested again, and publishes it, from this job, for gfx's handler to
// publish as CapturedEvent.
func (d *device) deliverCapture(capture CapturedEvent) {
	d.mapMutex.Lock()
	if index := slices.Index(d.capturingWindows, capture.Window); index >= 0 {
		d.capturingWindows = slices.Delete(d.capturingWindows, index, index+1)
	}
	d.mapMutex.Unlock()
	d.kernel.Publish(captureDeliveryEvent{captured: capture})
}

// dropPendingMap drops buffer's pending map, if it has one.
func (d *device) dropPendingMap(buffer Buffer) {
	d.mapMutex.Lock()
	defer d.mapMutex.Unlock()
	if index := slices.Index(d.pendingMaps, buffer); index >= 0 {
		d.pendingMaps = slices.Delete(d.pendingMaps, index, index+1)
	}
}

// failPendingMaps ends every map pending at a loss, from this job: a
// capture's is delivered CaptureStatusFailed, its buffer released, and any
// other publishes BufferMappedEvent with MapStatusFailed, its buffer kept. A
// pending map whose buffer was released publishes nothing.
func (d *device) failPendingMaps() {
	d.mapMutex.Lock()
	pending := d.pendingMaps
	captures := d.pendingCaptures
	d.pendingMaps = nil
	d.pendingCaptures = nil
	d.mapMutex.Unlock()
	for _, buffer := range pending {
		if d.buffers.check(buffer) != nil || slices.ContainsFunc(captures, func(capture CapturedEvent) bool { return capture.Buffer == buffer }) {
			continue
		}
		d.kernel.Publish(BufferMappedEvent{Buffer: buffer, Status: MapStatusFailed})
	}
	for _, capture := range captures {
		d.failCapture(capture)
	}
}

// releasePendingMaps unmaps and releases the buffer of every map still
// pending, captures' included, publishing nothing, and answers the unmaps'
// errors; a buffer released already is left alone, and a lost device unmaps
// nothing.
func (d *device) releasePendingMaps() error {
	d.mapMutex.Lock()
	pending := d.pendingMaps
	d.pendingMaps = nil
	d.pendingCaptures = nil
	d.mapMutex.Unlock()
	var errs []error
	for _, buffer := range pending {
		if d.buffers.check(buffer) != nil {
			continue
		}
		if !d.lost.Load() {
			errs = append(errs, d.backend.UnmapBuffer(buffer))
		}
		d.ReleaseBuffer(buffer)
	}
	return errors.Join(errs...)
}

// releaseAutoLayout releases layout, which createAutoLayout created, and the
// bind-group layouts it holds.
func (d *device) releaseAutoLayout(layout PipelineLayout) {
	descriptor, _ := d.pipelineLayouts.descriptor(layout)
	d.ReleasePipelineLayout(layout)
	for _, groupLayout := range descriptor.BindGroupLayouts {
		d.ReleaseBindGroupLayout(groupLayout)
	}
}

// groupDescriptors answers the descriptors of layout's bind-group layouts,
// none for the zero layout.
func (d *device) groupDescriptors(layout PipelineLayout) []BindGroupLayoutDescriptor {
	descriptor, _ := d.pipelineLayouts.descriptor(layout)
	groups := make([]BindGroupLayoutDescriptor, len(descriptor.BindGroupLayouts))
	for index, groupLayout := range descriptor.BindGroupLayouts {
		groups[index], _ = d.bindGroupLayouts.descriptor(groupLayout)
	}
	return groups
}

// groupLayout answers the layout of group in layout, or the zero layout when
// layout is stale or group past its last.
func (d *device) groupLayout(layout PipelineLayout, group uint32) BindGroupLayout {
	descriptor, _ := d.pipelineLayouts.descriptor(layout)
	if group >= uint32(len(descriptor.BindGroupLayouts)) {
		return BindGroupLayout{}
	}
	return descriptor.BindGroupLayouts[group]
}

// replaceBackend releases the lost backend device and requests a new one
// from the adapter or, when that fails, from a fresh adapter, as Start
// requested them. It answers the requests' errors when none answers a
// device, leaving d without a backend, and without an adapter when the
// fresh one failed too.
func (d *device) replaceBackend() error {
	if d.backend != nil {
		d.backend.Release()
		d.backend = nil
	}
	var refusal error
	if d.adapter != nil {
		backend, err := d.adapter.RequestDevice(d.deviceDescriptor)
		if err == nil {
			d.backend = backend
			return nil
		}
		refusal = err
		d.adapter.Release()
		d.adapter = nil
	}
	adapter, err := d.instance.RequestAdapter(d.adapterOptions)
	if err != nil {
		return errors.Join(refusal, err)
	}
	d.adapter = adapter
	d.adapterInfo = adapter.Info()
	backend, err := adapter.RequestDevice(d.deviceDescriptor)
	if err != nil {
		return errors.Join(refusal, err)
	}
	d.backend = backend
	return nil
}

// recreateObjects has the backend create every object the tables hold again,
// under its old handle, from what it was created with, in dependency order:
// buffers, textures, samplers and shader modules; views and bind group
// layouts; pipeline layouts; pipelines, the layouts gfx created for an
// "auto" one among those before them; bind groups. A creation that fails
// goes to LogErr, leaving its handle with no object; a device loss stops it,
// and is answered.
func (d *device) recreateObjects() error {
	logger := d.kernel.Logger()
	for _, recreate := range []func() error{
		func() error { return recreateResources(logger, &d.buffers, d.backend.CreateBuffer) },
		func() error { return recreateResources(logger, &d.textures, d.backend.CreateTexture) },
		func() error { return recreateResources(logger, &d.samplers, d.backend.CreateSampler) },
		func() error { return recreateResources(logger, &d.shaderModules, d.backend.CreateShaderModule) },
		func() error {
			return recreateResources(logger, &d.textureViews, func(view TextureView, source textureViewSource) error {
				return d.backend.CreateTextureView(view, source.texture, source.descriptor)
			})
		},
		func() error { return recreateResources(logger, &d.bindGroupLayouts, d.backend.CreateBindGroupLayout) },
		func() error { return recreateResources(logger, &d.pipelineLayouts, d.backend.CreatePipelineLayout) },
		func() error {
			return recreateResources(logger, &d.renderPipelines, func(pipeline RenderPipeline, source pipelineSource[RenderPipelineDescriptor]) error {
				return d.backend.CreateRenderPipeline(pipeline, source.descriptor)
			})
		},
		func() error {
			return recreateResources(logger, &d.computePipelines, func(pipeline ComputePipeline, source pipelineSource[ComputePipelineDescriptor]) error {
				return d.backend.CreateComputePipeline(pipeline, source.descriptor)
			})
		},
		func() error { return recreateResources(logger, &d.bindGroups, d.backend.CreateBindGroup) },
	} {
		if err := recreate(); err != nil {
			return err
		}
	}
	return nil
}

// recoverDevice recovers the lost device in place of beat's frame, under
// the write lock: it releases the backend device and requests a new one,
// creates every object again on it under its old handle, empty, marks every
// surface for configuring, forgets the frames' submits the lost device had
// not finished, and publishes DeviceRecoveredEvent from its job.
// A recovery that fails leaves the device lost and is tried again by the
// first beat a second or more later by Beat.TimeFromStart, with no limit;
// it goes to LogErrOnce, so a loss logs its failed recoveries once. A device
// not lost, recovered by an earlier beat, is left alone.
func recoverDevice(k *kernel.Kernel, beat metronome.Beat, deviceHandle kernel.Write[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	if !d.lost.Load() || beat.TimeFromStart < d.nextRecoveryAt {
		return kernel.NoResult{}
	}
	err := d.replaceBackend()
	if err == nil {
		err = d.recreateObjects()
	}
	if err != nil {
		d.nextRecoveryAt = beat.TimeFromStart + time.Second
		k.Logger().LogErrOnce(recoveryLogKey, fmt.Errorf("gfx: recovering the lost device: %w", err))
		return kernel.NoResult{}
	}
	k.Logger().ForgetLogOnce(recoveryLogKey)
	d.lost.Store(false)
	for _, window := range d.surfaces {
		d.frames.markUnconfigured(window)
	}
	d.frames.forgetSubmissions()
	k.Publish(DeviceRecoveredEvent{})
	return kernel.NoResult{}
}

// exclusiveDevice is the device as ElevateDeviceAccess answers it, with the
// operations only the write lock may reach.
type exclusiveDevice struct {
	*device
}

func (d exclusiveDevice) WaitIdle() error {
	if d.lost.Load() {
		return ErrDeviceLost
	}
	return d.checkLoss(d.backend.WaitIdle())
}

// RequestSurface replaces the surface request and marks every surface for
// configuring, so the next frame's configure reaches the backend before its
// acquire; a window opened after is configured from it.
func (d exclusiveDevice) RequestSurface(request SurfaceRequest) {
	d.surfaceRequest = request
	for _, window := range d.surfaces {
		d.frames.markUnconfigured(window)
	}
}

// textureViewSource is what a texture view was created from: its texture and
// its descriptor.
type textureViewSource struct {
	texture    Texture
	descriptor TextureViewDescriptor
}

// pipelineSource is what a pipeline was created with: its descriptor D, its
// layout always explicit, and whether gfx created that layout from the
// backend's reflection, an "auto" one released with the pipeline.
type pipelineSource[D any] struct {
	descriptor D
	autoLayout bool
}

// createResource acquires a handle in resources holding descriptor and has
// the backend create its object with createObject. When that fails, the
// handle is handed back, and the zero handle answered with the error.
func createResource[H ~struct{ handle }, D any](resources *table[H, D], descriptor D, createObject func(H, D) error) (H, error) {
	h := resources.acquire(descriptor)
	if err := createObject(h, descriptor); err != nil {
		_ = resources.retire(h)
		resources.release(h)
		var zero H
		return zero, err
	}
	return h, nil
}

// releaseResource makes h stale in resources, has d's backend release its
// object with releaseObject, unless d is lost, and hands its index out
// again, answering true; a stale h logs ErrReleased through d's logger, does
// nothing and answers false.
func releaseResource[H ~struct{ handle }, D any](d *device, resources *table[H, D], h H, releaseObject func(BackendDevice, H)) bool {
	if err := resources.retire(h); err != nil {
		d.kernel.Logger().LogErr(err)
		return false
	}
	if !d.lost.Load() {
		releaseObject(d.backend, h)
	}
	resources.release(h)
	return true
}

// recreateResources has the backend create the object of every handle
// resources holds again with createObject, under that handle, from what it
// was created with. A creation that fails goes to LogErr through logger,
// leaving its handle with no object, but for a device loss, which stops it
// and is answered.
func recreateResources[H ~struct{ handle }, D any](logger *kernel.Logger, resources *table[H, D], createObject func(H, D) error) error {
	for h, descriptor := range resources.heldHandles() {
		err := createObject(h, descriptor)
		if errors.Is(err, ErrDeviceLost) {
			return err
		}
		if err != nil {
			logger.LogErr(fmt.Errorf("gfx: recreating %T at %d: %w", h, struct{ handle }(h).index, err))
		}
	}
	return nil
}
