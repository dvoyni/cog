package mcp

import (
	"fmt"
	"reflect"
)

// Unavailable reports that a capability cannot run right now, in words meant
// for an agent to read and act on. It is an expected outcome, not a fault: the
// broker renders it as an ordinary tool result the agent reads before picking
// something else.
//
// Every reason should name the likely cause, not just the condition. "no frame
// was rendered within 2s — the game may be paused, minimised, or not rendering"
// is the model the others follow.
type Unavailable struct{ Reason string }

func (e Unavailable) Error() string { return e.Reason }

// ErrInvalidCapabilityName is the deferred failure of a capability whose name
// is not a lowercase identifier matching ^[a-z][a-z0-9_]*$.
type ErrInvalidCapabilityName struct {
	Name string
}

func (e ErrInvalidCapabilityName) Error() string {
	return fmt.Sprintf("capability name %q is not of the form [a-z][a-z0-9_]*", e.Name)
}

// ErrNonStructPayload is the deferred failure of a capability whose request or
// response type is not a struct. A JSON schema root has to be an object, so a
// one-value answer is a one-field struct.
type ErrNonStructPayload struct {
	Capability string
	Role       string
	Type       reflect.Type
}

func (e ErrNonStructPayload) Error() string {
	return fmt.Sprintf("capability %q has %s type %v, which is not a struct", e.Capability, e.Role, e.Type)
}
