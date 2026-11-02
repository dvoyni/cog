package kernel

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel/internal/concurrent"
)

// ErrQuitting is what Spawn answers once Quit has been called.
var ErrQuitting = errors.New("kernel: the supervisor is quitting")

// Supervisor runs the kernels of one application. It spawns and destroys
// kernels on demand, hands them to each other through GetKernel and
// Broadcast, runs their pinned jobs on the main thread, and owns the
// LogHandler every kernel writes to. Each kernel runs its own jobs on
// goroutines of its own.
//
// Kernels do not manage kernels. A plugin that must spawn, destroy, reach
// another kernel or quit is given the Supervisor in its config, and every
// method here is safe from any goroutine, jobs and handlers included.
type Supervisor struct {
	logHandler LogHandler

	// kernelsMu guards kernels and spawnOrder. Kernels come and go rarely and
	// are looked up rarely, so one mutex is enough.
	kernelsMu sync.Mutex
	// kernels is every live kernel: added once its plugins have started,
	// removed as its destroy begins.
	kernels map[KernelID]*Kernel
	// spawnOrder is the same kernels in the order they were spawned. It is
	// replaced on every change, never changed in place, so Broadcast takes it
	// under kernelsMu and publishes outside, without copying.
	spawnOrder []*Kernel
	lastID     atomic.Uint64
	// quitting is read anywhere, but set only under kernelsMu, so a Spawn that
	// checks it under kernelsMu before adding its kernel never adds one after
	// Quit has taken the map.
	quitting atomic.Bool
	done     chan struct{}

	// pinned is every pinned job ready to run, in the order kernels handed
	// them over. mainWake wakes WaitMainThread when one is pushed.
	pinned   concurrent.Queue[pinnedJob]
	mainWake chan struct{}
	// pinnedBatch is TickMainThread's buffer, reused between ticks. Only the
	// main thread touches it.
	pinnedBatch []pinnedJob
}

// pinnedJob is a pinned job of kernel, ready for the main thread.
type pinnedJob struct {
	kernel *Kernel
	job    JobHandle
}

// WaitMainThread blocks until a pinned job may be pending or Done is closed,
// and answers false once Done is closed. It may wake with nothing pending, in
// which case the next TickMainThread returns at once. It lets main idle
// without spinning a core between ticks.
func (s *Supervisor) WaitMainThread() bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case <-s.mainWake:
		return true
	case <-s.done:
		return false
	}
}

// TickMainThread runs every pinned job of every kernel that is pending, on the
// calling goroutine, which must be the main thread, and returns. It keeps
// going until none is pending, so a pinned job made ready by one that just
// ran runs in the same tick. It never waits: with nothing pending it returns
// at once. main calls it in its loop, parking in WaitMainThread between
// ticks, until Done is closed:
//
//	func init() { runtime.LockOSThread() } // keep main on the main thread
//
//	func main() {
//		s := kernel.NewSupervisor(nil)
//		// ... spawn kernels
//		for s.WaitMainThread() {
//			s.TickMainThread()
//		}
//	}
//
// Kernels pace themselves: a plugin with a timer publishes its own update
// events from its own goroutine, so kernels can tick at different rates and
// main never waits on any of them. A pinned job nobody ticks waits forever,
// and so does everything after it, its kernel's destroy included: main keeps
// ticking until Done is closed.
func (s *Supervisor) TickMainThread() {
	for {
		s.pinnedBatch = s.pinned.Drain(s.pinnedBatch[:0])
		if len(s.pinnedBatch) == 0 {
			return
		}
		for _, pinned := range s.pinnedBatch {
			pinned.kernel.runPinned(pinned.job)
		}
		// Let removed kernels go rather than keep them alive in the buffer.
		clear(s.pinnedBatch)
	}
}

// NewSupervisor creates a supervisor with no kernels, logging through logHandler,
// or through a new StdLogger if logHandler is nil.
func NewSupervisor(logHandler LogHandler) *Supervisor {
	if logHandler == nil {
		logHandler = &StdLogger{}
	}
	return &Supervisor{
		logHandler: logHandler,
		kernels:    map[KernelID]*Kernel{},
		done:       make(chan struct{}),
		mainWake:   make(chan struct{}, 1),
	}
}

// Spawn creates a kernel, registers plugins into it in order, handing each its
// part of config, the value under its Name, then starts them in the same
// order, all on the calling goroutine. The kernel is running when Spawn
// returns, and answers its ID.
//
// The kernel runs its jobs on workerCount goroutines of its own, which end
// with the kernel; runtime.GOMAXPROCS(0) keeps every CPU busy. A workerCount
// below 1, or two plugins of one name, panics.
//
// A workerCount of 1 runs every job one at a time, for a kernel that wants
// sequential execution, such as a server simulation. A job still waits for
// its dependencies, and runs in the order it became ready; locks never
// contend, since nothing runs alongside it. A task pinned to the main thread
// still runs there, and so may run alongside the kernel's worker.
//
// The kernel becomes live, reachable by GetKernel, Broadcast and Destroy,
// once every Start has returned. Before then it is not live: GetKernel
// answers nil for it, and a Destroy does nothing.
//
// If any Init or Start fails, the plugins already started are stopped in
// reverse, Spawn waits for every job they scheduled to finish, the plugins
// already initialized are destroyed in reverse, nothing of the kernel
// survives, and Spawn answers the zero KernelID and the first failure. A
// Spawn still running when Quit is called does the same once its plugins have
// started, and answers ErrQuitting.
func (s *Supervisor) Spawn(config map[PluginName]any, workerCount int, plugins ...Plugin) (KernelID, error) {
	if workerCount < 1 {
		panic(fmt.Sprintf("kernel: spawning a kernel with %d workers", workerCount))
	}
	for i, plugin := range plugins {
		for _, earlier := range plugins[:i] {
			if earlier.Name() == plugin.Name() {
				panic(fmt.Sprintf("kernel: spawning a kernel with two plugins named %q", plugin.Name()))
			}
		}
	}
	if s.quitting.Load() {
		return 0, ErrQuitting
	}
	id := KernelID(s.lastID.Add(1))
	k := newKernel(s, id, config, workerCount, plugins)
	if err := k.init(); err != nil {
		return 0, err
	}

	s.kernelsMu.Lock()
	if s.quitting.Load() {
		s.kernelsMu.Unlock()
		k.destroy()
		return 0, ErrQuitting
	}
	s.kernels[id] = k
	// Appended to a clipped slice, so the append copies and a Broadcast still
	// holding the old one is unaffected.
	s.spawnOrder = append(slices.Clip(s.spawnOrder), k)
	s.kernelsMu.Unlock()
	return id, nil
}

// GetKernel returns the live kernel id, or nil if there is none: if id was
// never spawned, is still spawning, or is being destroyed. It is how code
// outside the kernels, such as the main loop, publishes into a kernel and
// waits on its jobs, and how a plugin reaches another kernel.
//
// Publishing into another kernel is the whole of interop between kernels: the
// event is copied in and published there like any event of its own, with its
// handlers on the calling goroutine and its jobs in that kernel. It must not
// refer to anything the publishing kernel owns: a field such as a JobHandle
// arrives meaningless, and keeping such fields out is the publisher's
// business. A handler that publishes back in reply recurses, so an exchange
// between kernels needs a condition that ends it.
func (s *Supervisor) GetKernel(id KernelID) *Kernel {
	s.kernelsMu.Lock()
	defer s.kernelsMu.Unlock()
	return s.kernels[id]
}

// Broadcast publishes event in every live kernel, the calling kernel's own
// included, one after another in spawn order, with Kernel.Publish: each
// kernel's handlers run on the calling goroutine, and Broadcast returns once
// the last have. A kernel spawned or destroyed meanwhile may or may not get
// it. Kernels where nothing subscribes to E drop it silently.
//
// The rules of GetKernel's interop hold for every kernel: event must not
// refer to anything one kernel owns. Broadcast allocates nothing.
func (s *Supervisor) Broadcast[E any](event E) {
	s.kernelsMu.Lock()
	kernels := s.spawnOrder
	s.kernelsMu.Unlock()
	// Published outside the lock, so handlers may spawn, destroy and
	// broadcast. A kernel whose destroy begins meanwhile publishes nothing
	// once its plugins have stopped.
	for _, k := range kernels {
		k.Publish(event)
	}
}

// Destroy asks kernel id to stop and returns at once. The kernel stops being
// live, stops its plugins in reverse, finishes the jobs it already has,
// including those its plugins' Stop scheduled, and is gone. Destroying an id
// that is not live does nothing. Because it never waits, a kernel's own job
// may destroy it.
func (s *Supervisor) Destroy(id KernelID) {
	s.kernelsMu.Lock()
	k := s.kernels[id]
	if k != nil {
		delete(s.kernels, id)
		s.spawnOrder = slices.DeleteFunc(slices.Clone(s.spawnOrder), func(other *Kernel) bool { return other == k })
	}
	s.kernelsMu.Unlock()
	if k != nil {
		go k.destroy()
	}
}

// pushPinned hands pinned job of kernel to the main thread, and wakes
// WaitMainThread.
func (s *Supervisor) pushPinned(kernel *Kernel, job JobHandle) {
	s.pinned.Push(pinnedJob{kernel: kernel, job: job})
	select {
	case s.mainWake <- struct{}{}:
	default: // a wake is already pending
	}
}

// Quit ends the application: it destroys every live kernel, refuses any
// further Spawn, and closes Done once those kernels are gone. It returns at
// once, so a kernel's own job may call it, and is safe to call twice. The
// supervisor lives until Quit, with or without kernels.
//
// A Spawn already running destroys its kernel once its plugins have started,
// and answers ErrQuitting. Done does not wait for that kernel, nor for one a
// Destroy had already begun stopping.
func (s *Supervisor) Quit() {
	s.kernelsMu.Lock()
	if s.quitting.Load() {
		s.kernelsMu.Unlock()
		return
	}
	s.quitting.Store(true)
	kernels := s.kernels
	s.kernels = map[KernelID]*Kernel{}
	s.spawnOrder = nil
	s.kernelsMu.Unlock()

	go func() {
		var wait sync.WaitGroup
		for _, k := range kernels {
			wait.Go(k.destroy)
		}
		wait.Wait()
		close(s.done)
	}()
}

// Done is closed once Quit has finished. A kernel destroyed on its own, even
// the last one, does not close it.
func (s *Supervisor) Done() <-chan struct{} { return s.done }
