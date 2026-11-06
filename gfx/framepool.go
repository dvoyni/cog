package gfx

import (
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
// a backend call.
type framePool struct {
	mutex sync.Mutex
	slots []frameSlot
	// freeSlots are the released slots, handed out before new ones.
	freeSlots []uint32
	// lastPresent is the present of the newest frame, which the next acquire
	// and every surface release follow.
	lastPresent kernel.JobHandle
	// closingWindows are the windows closed whose surfaces are still to be
	// released: no acquire takes their framebuffers.
	closingWindows []platform.Window
}

// frameSlot is one frame's state: the generation of the Frame it holds, and
// a framebuffer per window acquired, its slice reused.
type frameSlot struct {
	generation   uint32
	framebuffers []framebuffer
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
		pool.slots = append(pool.slots, frameSlot{generation: nextGeneration(0)})
	}
	return Frame{slot: index, generation: pool.slots[index].generation}, pool.lastPresent
}

// setLastPresent keeps present as the newest frame's.
func (pool *framePool) setLastPresent(present kernel.JobHandle) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	pool.lastPresent = present
}

// release makes frame stale, dropping its framebuffers, and hands its slot
// out again.
func (pool *framePool) release(frame Frame) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	slot := &pool.slots[frame.slot]
	slot.generation = nextGeneration(slot.generation)
	slot.framebuffers = slot.framebuffers[:0]
	pool.freeSlots = append(pool.freeSlots, frame.slot)
}

// addFramebuffer keeps acquired as one of frame's framebuffers.
func (pool *framePool) addFramebuffer(frame Frame, acquired framebuffer) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	slot := &pool.slots[frame.slot]
	slot.framebuffers = append(slot.framebuffers, acquired)
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

// draw is Handlers.Draw: it acquires a frame, schedules the pinned acquire
// of its framebuffers after in and the last present, publishes DrawEvent
// with that acquire as Ready, and schedules the pinned present after the
// nested publication's out dependency, which it keeps as the last present
// and returns.
func (p *Plugin) draw(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event RenderEvent) (RenderEvent, kernel.JobHandle) {
	frame, lastPresent := p.device.frames.acquire()
	acquired := ex.Schedule(p.acquireTask, frame, in, lastPresent).Handle()
	_, out := ex.Publish(acquired, DrawEvent{Beat: event.Beat, Frame: frame, Ready: acquired})
	present := ex.Schedule(p.presentTask, frame, out).Handle()
	p.device.frames.setLastPresent(present)
	return event, present
}

// acquireFramebuffers acquires the framebuffer of every window with a surface
// and not closing, pinned to the main thread under the read lock. A window
// whose acquire answers no framebuffer, or an error, which goes to LogErr,
// has none this frame.
func acquireFramebuffers(k *kernel.Kernel, frame Frame, deviceHandle kernel.Read[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	for _, window := range d.surfaces {
		if d.frames.isClosing(window) {
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

// presentFramebuffers presents each framebuffer frame acquired, pinned to the
// main thread under the read lock, makes its view stale, and releases the
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
