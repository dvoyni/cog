// Package recording is the recording adapter: a gfx backend that records
// every call gfx makes instead of drawing, for tests to assert on. A test's
// own plugin binds it with configurator.BindSocket[gfx.InstanceSocket].
//
// It checks a fixed subset of what wgpu rejects (a handle missing at its
// index, usage flags, write and copy alignment, pass state) and answers what
// a real backend would. Buffers hold bytes, which writes, buffer copies and
// clears apply to; textures hold no texels, and captures complete
// zero-filled. Real pixels without a GPU come from gogpu on wgpu's software
// adapter, not from here.
package recording

import (
	"slices"
	"sync"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// Instance is the adapter's gfx.InstanceSocket. It records under a mutex,
// since gfx calls it from many goroutines.
type Instance struct {
	mutex     sync.Mutex
	recording Recording
	// answers are the answers AnswerNext queued, by kind, the next first.
	answers map[CallKind][]Answer
}

// NewInstance returns an adapter with an empty recording.
func NewInstance() *Instance { return &Instance{answers: map[CallKind][]Answer{}} }

// Recording answers a copy of what was recorded so far; a test reads it once
// the kernel has settled.
func (instance *Instance) Recording() *Recording {
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	return &Recording{
		Calls:       slices.Clone(instance.recording.Calls),
		Submissions: slices.Clone(instance.recording.Submissions),
	}
}

// AnswerNext makes the next call of kind answer answer instead of what it
// would: an AcquireStatus for an acquire, a failure for a map, an error,
// gfx.ErrDeviceLost included, for any call. Answers given for one kind are
// used in the order given.
func (instance *Instance) AnswerNext(kind CallKind, answer Answer) {
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	instance.answers[kind] = append(instance.answers[kind], answer)
}

// RequestAdapter answers the recording adapter.
func (instance *Instance) RequestAdapter(options gfx.RequestAdapterOptions) (gfx.BackendAdapter, error) {
	if err := instance.record(CallKindRequestAdapter, options).Error; err != nil {
		return nil, err
	}
	return &adapter{instance: instance}, nil
}

// CreateSurface records window's surface.
func (instance *Instance) CreateSurface(window platform.Window, native platform.NativeHandle) error {
	panic("gfx listing: no bodies")
}

// ReleaseSurface records window's surface released.
func (instance *Instance) ReleaseSurface(window platform.Window) { panic("gfx listing: no bodies") }

// Release records the instance released.
func (instance *Instance) Release() { instance.record(CallKindReleaseInstance, nil) }

// record records a call of kind with arguments, and answers the answer
// AnswerNext queued for it, with the error recorded, or the zero Answer.
func (instance *Instance) record(kind CallKind, arguments any) Answer {
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	var answer Answer
	if queued := instance.answers[kind]; len(queued) > 0 {
		answer = queued[0]
		instance.answers[kind] = queued[1:]
	}
	instance.recording.Calls = append(instance.recording.Calls, Call{Kind: kind, Arguments: arguments, Error: answer.Error})
	return answer
}
