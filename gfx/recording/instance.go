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
	"fmt"
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
	// surfaces holds each window's surface, as the native handle it was
	// created from.
	surfaces map[platform.Window]platform.NativeHandle
	// answers are the answers AnswerNext queued, by kind, the next first.
	answers map[CallKind][]Answer
}

// NewInstance returns an adapter with an empty recording.
func NewInstance() *Instance {
	return &Instance{surfaces: map[platform.Window]platform.NativeHandle{}, answers: map[CallKind][]Answer{}}
}

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
	if err := instance.record(CallKindRequestAdapter, options, nil).Error; err != nil {
		return nil, err
	}
	return &adapter{instance: instance}, nil
}

// CreateSurface records the creation and keeps window's surface; an error
// when window already has one.
func (instance *Instance) CreateSurface(window platform.Window, native platform.NativeHandle) error {
	return instance.record(CallKindCreateSurface, CreateSurfaceArguments{Window: window, Native: native}, func() error {
		if _, exists := instance.surfaces[window]; exists {
			return fmt.Errorf("recording: %v already has a surface", window)
		}
		instance.surfaces[window] = native
		return nil
	}).Error
}

// ReleaseSurface records the release and drops window's surface; the error
// recorded when window has none.
func (instance *Instance) ReleaseSurface(window platform.Window) {
	instance.record(CallKindReleaseSurface, window, func() error {
		if _, exists := instance.surfaces[window]; !exists {
			return fmt.Errorf("recording: %v has no surface", window)
		}
		delete(instance.surfaces, window)
		return nil
	})
}

// Release records the instance released.
func (instance *Instance) Release() { instance.record(CallKindReleaseInstance, nil, nil) }

// record records a call of kind with arguments, and answers the answer
// AnswerNext queued for it, with the error recorded, or the zero Answer.
// Unless that answer is an error, apply, when given, makes the call's effect
// under the mutex, and its error is the one recorded and answered.
func (instance *Instance) record(kind CallKind, arguments any, apply func() error) Answer {
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	var answer Answer
	if queued := instance.answers[kind]; len(queued) > 0 {
		answer = queued[0]
		instance.answers[kind] = queued[1:]
	}
	if answer.Error == nil && apply != nil {
		answer.Error = apply()
	}
	instance.recording.Calls = append(instance.recording.Calls, Call{Kind: kind, Arguments: arguments, Error: answer.Error})
	return answer
}
