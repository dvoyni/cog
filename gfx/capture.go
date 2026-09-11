package gfx

import (
	"image"
	"sync"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// maxCaptureAmount caps one request at sixty stills, and maxCaptureSpan caps
// the ticks they span. interval is what buys a long window, never amount. The
// span figure is app.TimeRequest's step cap, deliberately the same number so
// there is one figure to remember.
const (
	maxCaptureAmount = 60
	maxCaptureSpan   = 600
)

// GpuCapture is one completed readback: either the mapped bytes or the reason
// there are none. Pixels carries the GPU's own row padding, which BytesPerRow
// describes and Image removes; a backend never sees an image.Image.
//
// One struct carries success and failure so that a caller cannot handle one and
// forget the other, which is how a capture that never arrives becomes a hang
// somewhere far away.
type GpuCapture struct {
	Pixels        []byte
	Width, Height int
	Format        TextureFormat
	BytesPerRow   int
	Err           error
}

// Image un-strides the captured bytes into an image. It reports nil for a
// failed capture and for any format that is not 8-bit RGBA, which is every
// format a backend is allowed to hand back.
//
// image.NRGBA rather than image.RGBA: FormatRGBA8 is straight-alpha, and
// image.RGBA is premultiplied, so the obvious type would misread every
// translucent pixel. No colour conversion happens here in either case - an
// sRGB frame buffer's bytes are already what a PNG wants.
func (c GpuCapture) Image() image.Image {
	if c.Err != nil || !captureFormatSupported(c.Format) {
		return nil
	}
	if c.Width <= 0 || c.Height <= 0 {
		return nil
	}
	row := c.Width * 4
	if c.BytesPerRow < row || len(c.Pixels) < (c.Height-1)*c.BytesPerRow+row {
		return nil
	}
	picture := image.NewNRGBA(image.Rect(0, 0, c.Width, c.Height))
	for y := range c.Height {
		src := y * c.BytesPerRow
		dst := y * picture.Stride
		copy(picture.Pix[dst:dst+row], c.Pixels[src:src+row])
	}
	return picture
}

// captureFormatSupported reports whether a format is the 8-bit RGBA a capture
// can be an image of. Depth is not: it is a float field needing a range to be
// legible, which is a visualization question rather than a readback one.
func captureFormatSupported(format TextureFormat) bool {
	switch format.Resolve() {
	case FormatRGBA8, FormatRGBA8Srgb:
		return true
	default:
		return false
	}
}

// ArmCaptureCmd arms a readback of one colour target and hands back the wait.
// It is ordinary gfx API: anything holding a kernel handle may arm a capture,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because GpuCapture carries Err. The alternative - a channel passed in with
// the request - leaves gfx unable to refuse a second arm synchronously.
type ArmCaptureCmd kernel.Command[ArmCaptureRequest, ArmCaptureResponse]

// ArmCaptureRequest names the target and how many stills to take of it.
type ArmCaptureRequest struct {
	// Target is what to read back: the frame buffer, or any colour texture the
	// frame rendered into.
	Target GpuCaptureDesc
	// Amount is how many stills to write; zero means one, and the maximum is
	// sixty.
	Amount int
	// Interval is how many ticks apart the stills are; zero means one.
	// Amount x Interval may not exceed six hundred ticks.
	Interval int
	// Paused says the caller knows the engine's tick source is stopped, so no
	// tick can begin after this request and the last completed tick already is
	// the present. gfx does not read the tick source itself: pausing belongs to
	// the host that owns the loop, and gfx must not require a host to exist.
	//
	// A paused capture is served from the next render and costs no tick, which
	// is what makes two captures taken under one pause byte-identical. A burst
	// is refused while paused, because there would be nothing new to photograph.
	Paused bool
}

// ArmCaptureResponse hands back the wait and the window size.
type ArmCaptureResponse struct {
	// Done receives one GpuCapture per still, in order, and is buffered to
	// Amount so the render thread never blocks on a caller that walked away.
	Done <-chan GpuCapture
	// Viewport is the window as of the arm, which a capability body cannot read
	// for itself. The pixel dimensions always come from the capture, so a
	// window resized inside the capture's two-frame window reports a stale
	// window size but never mis-describes the image.
	Viewport app.Viewport
}

// captureRequest is one live capture or burst: where its stills go, and how far
// through them the engine has got.
type captureRequest struct {
	target   GpuCaptureDesc
	amount   int
	interval int
	// bounds counts the stills bound to a tick so far, and delivered the ones
	// whose pixels have been sent. Ordinals are contiguous from zero, so a
	// short burst is a prefix rather than a set with holes in it.
	bounds    int
	delivered int
	// ticks is how many more ticks must begin before the next still binds.
	ticks int
	done  chan GpuCapture
}

// captureState is gfx's one capture slot. A still moves through it in four
// stages, and each stage holds at most one, which is what keeps a burst from
// outrunning the single readback the backend allows:
//
//	pending  - waiting for a tick to begin after the request
//	armed    - a tick has begun; it binds when that tick completes
//	bound    - it rides the next render, whichever queue that render draws
//	inflight - the copy is encoded; its readback resolves a frame later
//
// The pending stage is what makes the guarantee true: a capture shows the game
// as of a tick that *began* after the request, so an arm landing inside a tick
// already running waits for the next one rather than binding to it.
//
// It is plugin-owned state with its own lock rather than a kernel resource,
// which is the one thing here that is forced rather than chosen: shutdown has
// to complete a waiting capture, Stop runs after the scheduler has stopped,
// and a stopped scheduler grants no locks - so a resource would be unreachable
// from the very path abandonment exists for.
type captureState struct {
	mu                              sync.Mutex
	request                         *captureRequest
	pending, armed, bound, inflight bool
}

// arm installs a request, or reports that one is already live. Refusing here,
// synchronously, is the first of the two refusal sites: the backend refuses a
// second in-flight map as well, because capture is a public gfx feature and a
// game's own code may arm one.
func (s *captureState) arm(request ArmCaptureRequest) (*captureRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request != nil {
		return nil, ErrCaptureBusy{}
	}
	amount := max(request.Amount, 1)
	interval := max(request.Interval, 1)
	switch {
	case amount > maxCaptureAmount:
		return nil, ErrCaptureAmount{Amount: amount, Max: maxCaptureAmount}
	case amount*interval > maxCaptureSpan:
		return nil, ErrCaptureSpan{Ticks: amount * interval, Max: maxCaptureSpan}
	case amount > 1 && request.Paused:
		return nil, ErrCaptureBurstPaused{}
	}
	live := &captureRequest{
		target: request.Target, amount: amount, interval: interval,
		done: make(chan GpuCapture, amount),
	}
	s.request = live
	// A paused engine will complete no further tick, so the last one already is
	// the present and the still binds straight to the next render. The
	// guarantee is satisfied vacuously rather than weakened.
	s.pending, s.armed, s.bound, s.inflight = !request.Paused, false, request.Paused, false
	if request.Paused {
		live.bounds = 1
	}
	return live, nil
}

// beginTick moves a waiting still into the tick that has just begun, and
// counts down the interval between the stills of a burst. It runs ahead of
// every other subscriber to app.UpdateEvent, before anything records, so the
// tick it admits a still to is one that began after the arm.
func (s *captureState) beginTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.pending || s.armed {
		return
	}
	if s.request.ticks > 0 {
		s.request.ticks--
		return
	}
	s.pending, s.armed = false, true
}

// endTick binds the still admitted by beginTick to the tick that has just
// completed. It runs last, beside the queue swap, so the still rides the ready
// slot: if a newer recorded queue displaces the pending one before a render,
// the capture goes with the newer queue and the guarantee only strengthens.
func (s *captureState) endTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.armed || s.bound {
		return
	}
	s.armed, s.bound = false, true
	s.request.bounds++
	if s.request.bounds < s.request.amount {
		s.pending, s.request.ticks = true, s.request.interval-1
	}
}

// target reports what the frame about to be rendered should read back.
func (s *captureState) target() (GpuCaptureDesc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.bound {
		return GpuCaptureDesc{}, false
	}
	return s.request.target, true
}

// encoded records that the render just executed carried the capture op, so its
// result is now the backend's to hand back.
func (s *captureState) encoded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.bound {
		return
	}
	s.bound, s.inflight = false, true
}

// deliver hands one completed readback to whoever armed it, and ends the
// request when it was the last. The send is non-blocking: the channel is
// buffered to the whole burst, so a full one means the waiter has gone, and a
// value nobody receives is simply collected.
//
// A failure ends the request wherever it lands. A burst truncates rather than
// failing, and the ordinals already delivered are the short success.
func (s *captureState) deliver(capture GpuCapture) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.inflight {
		return
	}
	s.inflight = false
	select {
	case s.request.done <- capture:
	default:
	}
	s.request.delivered++
	if capture.Err != nil || s.request.delivered >= s.request.amount {
		s.clear()
	}
}

// abandon completes a live request as a failure on the channel a result would
// have used. Shutdown is the case it exists for: a capture armed in frame N
// resolves in N+1, and if the window closes between them the waiter would
// otherwise learn nothing until its client's idle abort.
func (s *captureState) abandon() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil {
		return
	}
	select {
	case s.request.done <- GpuCapture{Err: ErrCaptureAbandoned{}}:
	default:
	}
	s.clear()
}

// clear drops the request and every stage token with it.
func (s *captureState) clear() {
	s.request = nil
	s.pending, s.armed, s.bound, s.inflight = false, false, false, false
}
