package gfx

import (
	"cmp"
	"fmt"
	"slices"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// framePool is gfx's pool of frame slots and the chain the frames draw on:
// each frame's acquire follows the last present, since a surface holds one
// framebuffer at a time. Its slots are acquired in the RenderEvent handler
// and released after the present, so it grows only while more beats are in
// flight than it holds. Its mutex is held only for bookkeeping, never across
// a backend call, and guards everything but what each slot's own mutex does.
type framePool struct {
	mutex sync.Mutex
	// slots are pointers, so the slice grows without copying a slot's mutex.
	slots []*frameSlot
	// freeSlots are the released slots, handed out before new ones.
	freeSlots []uint32
	// lastPresent is the present of the newest frame, which the next acquire
	// and every surface release follow.
	lastPresent kernel.JobHandle
	// closingWindows are the windows closed whose surfaces are still to be
	// released: no acquire takes their framebuffers.
	closingWindows []platform.Window
	// unconfiguredWindows are the windows whose surfaces are marked for
	// configuring before the next acquire.
	unconfiguredWindows []platform.Window
}

// frameSlot is one frame's state: the generation of the Frame it holds, a
// framebuffer per window acquired, and the encoders taken for it, each slice
// reused. Its mutex guards the encoders and the flushed flag, held only for
// the check and the append, and is held as well as the pool's to change the
// generation, so either guards reading it.
type frameSlot struct {
	mutex        sync.Mutex
	generation   uint32
	framebuffers []framebuffer
	// encoders are the frame's encoders, in the order taken until the flush
	// sorts them.
	encoders []frameEncoder
	// commandBuffers are the flush's finished encoders, kept only to reuse
	// the slice.
	commandBuffers []CommandBuffer
	// flushed is set by the flush: no encoder is taken after it.
	flushed bool
}

// frameEncoder is one encoder a frame took, at its submit order.
type frameEncoder struct {
	order   SubmitOrder
	encoder StandaloneCommandEncoder
}

// framebuffer is one window's framebuffer in a frame: the view gfx handed the
// backend to store it at.
type framebuffer struct {
	window platform.Window
	view   TextureView
}

// acquire answers a frame in a free slot, or a new one when none is free,
// and the present it is to be acquired after.
func (pool *framePool) acquire() (frame Frame, lastPresent kernel.JobHandle) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	var index uint32
	if count := len(pool.freeSlots); count > 0 {
		index = pool.freeSlots[count-1]
		pool.freeSlots = pool.freeSlots[:count-1]
	} else {
		index = uint32(len(pool.slots))
		pool.slots = append(pool.slots, &frameSlot{generation: nextGeneration(0)})
	}
	return Frame{slot: index, generation: pool.slots[index].generation}, pool.lastPresent
}

// setLastPresent keeps present as the newest frame's.
func (pool *framePool) setLastPresent(present kernel.JobHandle) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	pool.lastPresent = present
}

// release makes frame stale, dropping its framebuffers and encoders, and
// hands its slot out again.
func (pool *framePool) release(frame Frame) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	slot := pool.slots[frame.slot]
	slot.mutex.Lock()
	defer slot.mutex.Unlock()
	slot.generation = nextGeneration(slot.generation)
	slot.framebuffers = slot.framebuffers[:0]
	clear(slot.encoders)
	slot.encoders = slot.encoders[:0]
	clear(slot.commandBuffers)
	slot.commandBuffers = slot.commandBuffers[:0]
	slot.flushed = false
	pool.freeSlots = append(pool.freeSlots, frame.slot)
}

// addFramebuffer keeps acquired as one of frame's framebuffers.
func (pool *framePool) addFramebuffer(frame Frame, acquired framebuffer) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	slot := pool.slots[frame.slot]
	slot.framebuffers = append(slot.framebuffers, acquired)
}

// addEncoder keeps taken as one of frame's encoders; ErrFrameFlushed when
// frame is flushed or stale, and ErrOrderTaken when it already has an
// encoder at taken's order.
func (pool *framePool) addEncoder(frame Frame, taken frameEncoder) error {
	pool.mutex.Lock()
	if frame.slot >= uint32(len(pool.slots)) {
		pool.mutex.Unlock()
		return ErrFrameFlushed
	}
	slot := pool.slots[frame.slot]
	pool.mutex.Unlock()
	slot.mutex.Lock()
	defer slot.mutex.Unlock()
	if slot.generation != frame.generation || slot.flushed {
		return ErrFrameFlushed
	}
	for _, encoder := range slot.encoders {
		if encoder.order == taken.order {
			return ErrOrderTaken
		}
	}
	slot.encoders = append(slot.encoders, taken)
	return nil
}

// flush marks frame flushed, so it takes no more encoders, and answers its
// slot: nothing but the flush touches the slot's encoders and command
// buffers until the frame is released.
func (pool *framePool) flush(frame Frame) *frameSlot {
	pool.mutex.Lock()
	slot := pool.slots[frame.slot]
	pool.mutex.Unlock()
	slot.mutex.Lock()
	defer slot.mutex.Unlock()
	slot.flushed = true
	return slot
}

// framebuffers answers frame's framebuffers, for its present: nothing else
// changes them until it releases the frame.
func (pool *framePool) framebuffers(frame Frame) []framebuffer {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	return pool.slots[frame.slot].framebuffers
}

// framebuffer answers the view of window's framebuffer in frame, or the zero
// view when frame acquired none for it or is stale.
func (pool *framePool) framebuffer(frame Frame, window platform.Window) TextureView {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	if frame.slot >= uint32(len(pool.slots)) || pool.slots[frame.slot].generation != frame.generation {
		return TextureView{}
	}
	for _, acquired := range pool.slots[frame.slot].framebuffers {
		if acquired.window == window {
			return acquired.view
		}
	}
	return TextureView{}
}

// markClosing keeps window from every acquire from now on, and answers the
// present its surface's release is to follow.
func (pool *framePool) markClosing(window platform.Window) kernel.JobHandle {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	pool.closingWindows = append(pool.closingWindows, window)
	return pool.lastPresent
}

// isClosing answers whether window is marked closing.
func (pool *framePool) isClosing(window platform.Window) bool {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	return slices.Contains(pool.closingWindows, window)
}

// unmarkClosing drops window's closing mark, once its surface is released.
func (pool *framePool) unmarkClosing(window platform.Window) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	if index := slices.Index(pool.closingWindows, window); index >= 0 {
		pool.closingWindows = slices.Delete(pool.closingWindows, index, index+1)
	}
}

// markUnconfigured marks window's surface for configuring before the next
// acquire.
func (pool *framePool) markUnconfigured(window platform.Window) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	if !slices.Contains(pool.unconfiguredWindows, window) {
		pool.unconfiguredWindows = append(pool.unconfiguredWindows, window)
	}
}

// hasUnconfigured answers whether any surface is marked for configuring.
func (pool *framePool) hasUnconfigured() bool {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	return len(pool.unconfiguredWindows) > 0
}

// takeUnconfigured answers the windows marked for configuring, and drops
// their marks.
func (pool *framePool) takeUnconfigured() []platform.Window {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	windows := pool.unconfiguredWindows
	pool.unconfiguredWindows = nil
	return windows
}

// unmarkUnconfigured drops window's configuring mark, once its surface is
// released.
func (pool *framePool) unmarkUnconfigured(window platform.Window) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	if index := slices.Index(pool.unconfiguredWindows, window); index >= 0 {
		pool.unconfiguredWindows = slices.Delete(pool.unconfiguredWindows, index, index+1)
	}
}

// draw is Handlers.Draw: it acquires a frame and, when a surface is marked
// for it, schedules the configure after in and the last present; then the
// acquire of its framebuffers after in, the last present and that configure,
// publishes DrawEvent with that acquire as Ready, and schedules the flush
// after the nested publication's out dependency, then the present, which it
// keeps as the last present and returns.
func (p *Plugin) draw(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event RenderEvent) (RenderEvent, kernel.JobHandle) {
	frame, lastPresent := p.device.frames.acquire()
	configured := lastPresent
	if p.device.frames.hasUnconfigured() {
		configured = ex.Schedule(p.tasks.ConfigureSurfaces, kernel.NoArg{}, in, lastPresent).Handle()
	}
	acquired := ex.Schedule(p.tasks.AcquireFramebuffers, frame, in, configured).Handle()
	_, out := ex.Publish(acquired, DrawEvent{Beat: event.Beat, Frame: frame, Ready: acquired})
	flushed := ex.Schedule(p.flushTask, frame, out).Handle()
	present := ex.Schedule(p.tasks.PresentFramebuffers, frame, flushed).Handle()
	p.device.frames.setLastPresent(present)
	return event, present
}

// acquireFramebuffers acquires the framebuffer of every window with a
// configured surface and not closing, under the read lock. A window whose acquire answers no framebuffer, or an error,
// which goes to LogErr, has none this frame.
func acquireFramebuffers(k *kernel.Kernel, frame Frame, deviceHandle kernel.Read[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	for _, window := range d.surfaces {
		if _, configured := d.configurations[window]; !configured || d.frames.isClosing(window) {
			continue
		}
		view := d.textureViews.acquire(textureViewSource{})
		status, err := d.backend.AcquireFramebuffer(window, view)
		if err != nil {
			k.Logger().LogErr(fmt.Errorf("gfx: acquiring the framebuffer of %v: %w", window, err))
		}
		if err != nil || (status != AcquireStatusAcquired && status != AcquireStatusSuboptimal) {
			_ = d.textureViews.retire(view)
			d.textureViews.release(view)
			continue
		}
		d.frames.addFramebuffer(frame, framebuffer{window: window, view: view})
	}
	return kernel.NoResult{}
}

// flushFrame finishes frame's encoders, under the read lock, and submits
// them once, sorted by submit order. An encoder whose Finish fails goes to
// LogErr and is dropped, and so does a failing submit. A frame with no
// encoder submits nothing.
func flushFrame(k *kernel.Kernel, frame Frame, deviceHandle kernel.Read[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	slot := d.frames.flush(frame)
	slices.SortFunc(slot.encoders, func(a, b frameEncoder) int { return cmp.Compare(a.order, b.order) })
	for _, taken := range slot.encoders {
		buffer, err := taken.encoder.Finish()
		if err != nil {
			k.Logger().LogErr(fmt.Errorf("gfx: finishing the encoder at submit order %d: %w", taken.order, err))
			continue
		}
		slot.commandBuffers = append(slot.commandBuffers, buffer)
	}
	if len(slot.commandBuffers) > 0 {
		if err := d.backend.Submit(slot.commandBuffers...); err != nil {
			k.Logger().LogErr(fmt.Errorf("gfx: submitting a frame: %w", err))
		}
	}
	return kernel.NoResult{}
}

// presentFramebuffers presents each framebuffer frame acquired, under the
// read lock, makes its view stale, and releases the
// frame. A present that fails goes to LogErr.
func presentFramebuffers(k *kernel.Kernel, frame Frame, deviceHandle kernel.Read[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	for _, acquired := range d.frames.framebuffers(frame) {
		if err := d.backend.Present(acquired.window); err != nil {
			k.Logger().LogErr(fmt.Errorf("gfx: presenting %v: %w", acquired.window, err))
		}
		_ = d.textureViews.retire(acquired.view)
		d.textureViews.release(acquired.view)
	}
	d.frames.release(frame)
	return kernel.NoResult{}
}
