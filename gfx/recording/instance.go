// Package recording is the recording adapter: a gfx backend that records
// every call gfx makes instead of drawing, for tests to assert on. A test's
// own plugin binds it with configurator.BindSocket[gfx.InstanceSocket].
//
// It checks a fixed subset of what wgpu rejects (a handle missing at its
// index, usage flags, write and copy alignment, pass state) and answers what
// a real backend would. Buffers hold bytes, which writes, buffer copies and
// clears apply to; textures hold no texels, and captures complete
// zero-filled. Real pixels without a GPU come from gogpu on wgpu's software
// adapter, not from here. Instance.DisableRecording keeps no call, for
// benchmarks and allocation gates, and Instance.EnableTimestamps writes
// timestamps, which wait on the wgpu fork for every other backend.
package recording

import (
	"fmt"
	"io/fs"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// Instance is the adapter's gfx.InstanceSocket. It records under a mutex,
// since gfx calls it from many goroutines.
type Instance struct {
	mutex     sync.Mutex
	recording Recording
	// recordingDisabled is set by DisableRecording; read without the mutex
	// by what decides whether to copy what it would keep.
	recordingDisabled atomic.Bool
	// timestampsEnabled is set by EnableTimestamps, and adapterRequested
	// by the first RequestAdapter, after which timestamps are fixed.
	timestampsEnabled atomic.Bool
	adapterRequested  atomic.Bool
	surfaces          map[platform.Window]surface
	// answers are the answers AnswerNext queued, by kind, the next first.
	answers map[CallKind][]Answer
}

// NewInstance returns an adapter with an empty recording.
func NewInstance() *Instance {
	return &Instance{surfaces: map[platform.Window]surface{}, answers: map[CallKind][]Answer{}}
}

// Recording answers a copy of what was recorded so far; a test reads it once
// the kernel has settled.
func (instance *Instance) Recording() *Recording {
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	return &Recording{
		Calls:          slices.Clone(instance.recording.Calls),
		Submissions:    slices.Clone(instance.recording.Submissions),
		submittedAfter: slices.Clone(instance.recording.submittedAfter),
	}
}

// DisableRecording makes the adapter keep no call from now on, each still
// answered and checked as before, so a benchmark or an allocation gate
// measures gfx and its users, not the recording: once warm, a frame of
// writes, bind groups, passes and draws allocates nothing in the adapter.
// Recording answers what was recorded before.
func (instance *Instance) DisableRecording() { instance.recordingDisabled.Store(true) }

// EnableTimestamps makes the adapter work timestamps as a backend whose
// device has them: its adapter reports gfx.FeatureFlagTimestampQuery, its
// device keeps timestamp query sets, and a pass's timestamp writes and a
// resolve of a query set apply at the submit, in order with its copies and
// clears. The ticks are made up: a command buffer's commands are one tick
// apart, from the tick the buffer submitted before it ended at, so a pass's
// timestamps are as many ticks apart as the commands it recorded, its
// beginning among them. It is called before the adapter is requested, and
// panics after.
func (instance *Instance) EnableTimestamps() {
	if instance.adapterRequested.Load() {
		panic("recording: enabling timestamps after the adapter was requested")
	}
	instance.timestampsEnabled.Store(true)
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
	instance.adapterRequested.Store(true)
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
		instance.surfaces[window] = surface{native: native}
		return nil
	}).Error
}

// ReleaseSurface records the release and drops window's surface; the error
// recorded when window has none, or holds a framebuffer not yet presented.
func (instance *Instance) ReleaseSurface(window platform.Window) {
	instance.record(CallKindReleaseSurface, window, func() error {
		windowSurface, exists := instance.surfaces[window]
		if !exists {
			return fmt.Errorf("recording: %v has no surface", window)
		}
		if windowSurface.framebuffer != (gfx.TextureView{}) {
			return fmt.Errorf("recording: releasing the surface of %v, its framebuffer not presented", window)
		}
		delete(instance.surfaces, window)
		return nil
	})
}

// PreprocessShader records the call and answers the text of path in files
// as it is, no directive handled, or the error reading it: the recording
// adapter preprocesses nothing, and a test that needs real preprocessing
// calls gogpu/shader itself.
func (instance *Instance) PreprocessShader(files fs.FS, path string, defines []gfx.ShaderDefine) (string, error) {
	var text []byte
	arguments := PreprocessShaderArguments{Path: path, Defines: slices.Clone(defines)}
	err := instance.record(CallKindPreprocessShader, arguments, func() error {
		var err error
		text, err = fs.ReadFile(files, path)
		return err
	}).Error
	return string(text), err
}

// Release records the instance released.
func (instance *Instance) Release() { instance.record[any](CallKindReleaseInstance, nil, nil) }

// record records a call of kind with arguments, unless recording is
// disabled, and answers the answer
// AnswerNext queued for it, with the error recorded, or the zero Answer.
// Unless that answer is an error, apply, when given, makes the call's effect
// under the mutex, and its error is the one recorded and answered.
func (instance *Instance) record[A any](kind CallKind, arguments A, apply func() error) Answer {
	return instance.recordAnswered(kind, arguments, func(Answer) error {
		if apply == nil {
			return nil
		}
		return apply()
	})
}

// recordAnswered records as record does, handing apply the answer queued, so
// the call's effect can follow its status. arguments are made an interface
// only when kept, so a call recording nothing allocates nothing.
func (instance *Instance) recordAnswered[A any](kind CallKind, arguments A, apply func(answer Answer) error) Answer {
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	answer := instance.takeAnswer(kind)
	if answer.Error == nil {
		answer.Error = apply(answer)
	}
	if instance.keepsCalls() {
		instance.recording.Calls = append(instance.recording.Calls, Call{Kind: kind, Arguments: arguments, Status: answer.Status, Error: answer.Error})
	}
	return answer
}

// keepsCalls answers whether calls and commands are kept: recording is not
// disabled.
func (instance *Instance) keepsCalls() bool { return !instance.recordingDisabled.Load() }

// cloneKept answers a copy of values, which a kept call or command owns,
// when calls are kept, and values itself, which nothing keeps, otherwise.
func (instance *Instance) cloneKept[E any](values []E) []E {
	if !instance.keepsCalls() {
		return values
	}
	return slices.Clone(values)
}

// takeAnswer answers the next answer AnswerNext queued for kind, taking it,
// or the zero Answer; the caller holds the mutex.
func (instance *Instance) takeAnswer(kind CallKind) Answer {
	queued := instance.answers[kind]
	if len(queued) == 0 {
		return Answer{}
	}
	instance.answers[kind] = queued[1:]
	return queued[0]
}

// holdsFramebuffer answers whether view is a framebuffer acquired and not yet
// presented; the caller holds the mutex.
func (instance *Instance) holdsFramebuffer(view gfx.TextureView) bool {
	for _, windowSurface := range instance.surfaces {
		if windowSurface.framebuffer == view {
			return true
		}
	}
	return false
}

// framebufferUsage answers the usage of texture when it is a framebuffer
// acquired and not yet presented: RenderAttachment, and CopySrc when its
// acquire was captured; the caller holds the mutex.
func (instance *Instance) framebufferUsage(texture gfx.Texture) (usage gfx.TextureUsageFlags, isFramebuffer bool) {
	for _, windowSurface := range instance.surfaces {
		if windowSurface.framebufferTexture != texture {
			continue
		}
		usage = gfx.TextureUsageFlagRenderAttachment
		if windowSurface.captured {
			usage |= gfx.TextureUsageFlagCopySrc
		}
		return usage, true
	}
	return gfx.TextureUsageFlagNone, false
}

// surface is one window's surface: the native handle it was created from,
// whether it is configured, and the framebuffer acquired and not yet
// presented, its texture and view, zero when none is, and whether its
// acquire was captured.
type surface struct {
	native             platform.NativeHandle
	configured         bool
	framebufferTexture gfx.Texture
	framebuffer        gfx.TextureView
	captured           bool
}
