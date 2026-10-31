package kernel

// Supervisor runs the kernels of one application. It owns the worker pool
// every kernel's jobs run on, the Logger every kernel writes to, routes Send
// between kernels, and spawns and destroys kernels on demand.
//
// Kernels do not manage kernels. A plugin that must spawn, destroy, send or
// quit is given the Supervisor in its config, and every method here is safe
// from any goroutine, jobs and handlers included.
type Supervisor struct{}

// NewSupervisor creates a supervisor with no kernels, logging through logger,
// or through a new StdLogger if logger is nil.
func NewSupervisor(logger Logger) *Supervisor { return &Supervisor{} }

// Spawn creates a kernel, registers plugins into it in order, handing each the
// same config, then starts them in the same order, all on the calling
// goroutine. The kernel is running when Spawn returns, and answers its ID.
//
// If any Register or Start fails, the plugins already started are stopped in
// reverse, nothing of the kernel survives, and Spawn answers the zero
// KernelID and the failures.
func (s *Supervisor) Spawn(config any, plugins ...Plugin) (KernelID, error) { return 0, nil }

// SpawnSingleThreaded is Spawn for a kernel whose jobs all run one at a time,
// on one goroutine of its own locked to an OS thread, instead of on the pool.
// It is for a kernel that wants sequential execution, such as a server
// simulation. A job still waits for its dependencies, and runs in the order
// it became ready; locks never contend, since nothing runs alongside it. A
// task pinned to the main thread still runs there.
//
// The thread is the kernel's only goroutine, and ends with the kernel.
func (s *Supervisor) SpawnSingleThreaded(config any, plugins ...Plugin) (KernelID, error) {
	return 0, nil
}

// TickMainThread runs every pinned job of every kernel that is pending, on the
// calling goroutine, which must be the main thread, and returns. It never
// waits: with nothing pending it returns at once. main calls it in its loop
// until Done is closed:
//
//	func init() { runtime.LockOSThread() } // keep main on the main thread
//
//	func main() {
//		s := kernel.NewSupervisor(nil)
//		// ... spawn kernels
//		for {
//			s.TickMainThread()
//			select {
//			case <-s.Done():
//				return
//			default:
//			}
//		}
//	}
//
// Kernels pace themselves: a plugin with a timer publishes its own update
// events from its own goroutine, so kernels can tick at different rates and
// main never waits on any of them. A pinned job nobody ticks waits forever,
// and so does everything after it.
func (s *Supervisor) TickMainThread() {}

// GetKernel returns the live kernel id, or nil if there is none. It is how
// code outside the pool, such as the main loop, publishes into a kernel and
// waits on its jobs. A plugin that must reach another kernel uses Send, not
// GetKernel: kernels do not manage kernels.
func (s *Supervisor) GetKernel(id KernelID) *Kernel { return nil }

// Destroy asks kernel id to stop and returns at once. The kernel stops its
// plugins, finishes the jobs it already has, drops what is sent to it from
// then on, and is removed. Destroying an id that is not live does nothing.
// Because it never waits, a kernel's own job may destroy it.
func (s *Supervisor) Destroy(id KernelID) {}

// Send publishes event in kernel to, asynchronously. It is the whole of
// interop between kernels: event is copied across and published there like
// any event of that kernel's own, so it must not refer to anything another
// kernel owns. Sending to an id that is not live is logged; an event that
// arrives after its target has begun stopping is dropped.
//
// An event nothing in the target kernel subscribes to is dropped silently.
// Each kernel has one inbox per event type it subscribes to, made at
// registration, so sending never allocates.
//
// Send copies event as it is and does not look inside it. A field that means
// something only in the sending kernel, such as a JobHandle, arrives
// meaningless, and keeping such fields out is the sender's business.
func (s *Supervisor) Send[E any](to KernelID, event E) {}

// Quit ends the application: it destroys every live kernel, refuses any
// further Spawn, and closes Done once every kernel has been removed. It is
// safe from any goroutine and safe to call twice.
func (s *Supervisor) Quit() {}

// Done is closed once Quit has finished. A kernel destroyed on its own, even
// the last one, does not close it.
func (s *Supervisor) Done() <-chan struct{} { return nil }
