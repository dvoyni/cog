package kernel

// Signal tells code outside the kernel that a job has finished. It is made once
// and reused: Kernel.Notify arms it for one job at a time, and C delivers one
// value per finish. Its channel holds one value and is sent to without
// blocking, so a finish nobody has read yet absorbs the next.
//
// A Signal is how the main loop, or a test, waits on the kernel. A task or
// handler never waits: waiting on a worker can deadlock the kernel.
type Signal struct {
	ch chan struct{}
}

// NewSignal makes a Signal. It allocates, so it belongs at startup, not in a
// frame.
func NewSignal() *Signal { return &Signal{ch: make(chan struct{}, 1)} }

// C is the channel a finish is delivered on.
func (s *Signal) C() <-chan struct{} { return s.ch }

// send delivers one finish without blocking. A finish nobody has read yet
// absorbs this one.
func (s *Signal) send() {
	select {
	case s.ch <- struct{}{}:
	default:
	}
}
