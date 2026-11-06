package recording

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// AdapterInfo is what the recording adapter answers for Info.
var AdapterInfo = gfx.AdapterInfo{
	Name:       "recording",
	Vendor:     "cog",
	DeviceType: gfx.DeviceTypeCPU,
	Backend:    gfx.BackendEmpty,
}

// CallKind names a kind of backend call, for AnswerNext and for the calls
// a Recording holds.
type CallKind uint16

const (
	// CallKindRequestAdapter is InstanceSocket.RequestAdapter, its Arguments
	// the gfx.RequestAdapterOptions.
	CallKindRequestAdapter CallKind = iota
	// CallKindRequestDevice is BackendAdapter.RequestDevice, its Arguments
	// the gfx.DeviceDescriptor.
	CallKindRequestDevice
	// CallKindWaitIdle is BackendDevice.WaitIdle, with no Arguments.
	CallKindWaitIdle
	// CallKindReleaseDevice is BackendDevice.Release, with no Arguments.
	CallKindReleaseDevice
	// CallKindReleaseAdapter is BackendAdapter.Release, with no Arguments.
	CallKindReleaseAdapter
	// CallKindReleaseInstance is InstanceSocket.Release, with no Arguments.
	CallKindReleaseInstance
)

// Answer is what AnswerNext makes a call answer: Status for an acquire,
// Error for any call.
type Answer struct {
	Status uint8
	Error  error
}

// Recording is every call that reached the adapter, as typed values. The
// order of Submissions and of the surface calls is deterministic, since gfx
// sorts and chains them; the order of Calls from parallel tasks is not, so a
// test looks them up by handle.
type Recording struct {
	// Calls are the lifecycle, write, map and surface calls, each with its
	// handle and a deep copy of its descriptor.
	Calls       []Call
	Submissions []Submission
}

// Frames answers the submissions and surface calls between each acquire and
// its present.
func (recording *Recording) Frames() []Frame { panic("gfx listing: no bodies") }

// Call is one recorded call outside command buffers.
type Call struct {
	Kind CallKind
	// Arguments is the call's handle and descriptor, as the gfx types.
	Arguments any
	Error     error
}

// Submission is one submit's command buffers, in the order submitted.
type Submission struct {
	Buffers []CommandBuffer
}

// CommandBuffer is one finished encoder: its passes, and the copies and
// clears recorded outside them, in order.
type CommandBuffer struct {
	Commands []Command
}

// Command is one recorded encoder or pass command with its arguments; a
// pass's commands are under its BeginRenderPass or BeginComputePass.
type Command struct {
	Name      string
	Arguments any
	Commands  []Command
}

// Frame is one frame as the adapter saw it: each window's acquire status, the
// submissions, and the presents.
type Frame struct {
	Acquired    map[platform.Window]uint8
	Submissions []Submission
	Presented   []platform.Window
}
