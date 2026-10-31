package types

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

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
	return fmt.Sprintf("capability %q has %s type %s, which is not a struct", e.Capability, e.Role, kernel.TypeName(e.Type))
}
