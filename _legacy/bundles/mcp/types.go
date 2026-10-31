package mcp

import (
	"encoding"

	"github.com/dvoyni/cog/bundles/mcp/internal/types"
)

// Capability is a named, described, typed unit of engine functionality a
// Provider offers to an agent. It is fixed for the engine lifetime.
//
// It is a struct with unexported fields rather than an interface, and only
// Command and Func may construct one. That is what makes the capability-body
// rule unforgeable: no other package can implement Capability and slip work
// onto the broker's goroutine.
//
// Construction failures defer into its Err rather than panicking, because
// Capabilities returns a slice literal and has nowhere to return an error. The
// broker collects the failures at Start and fails composition with them.
//
// The broker reads it through Name, Description, RequestType, ResponseType,
// ReadOnly, Err and Invoke.
type Capability = types.Capability

// Option configures a capability at construction. The list is variadic on both
// constructors so later options do not break every provider, and the value it
// writes to is unexported so the set of options stays closed to this package.
type Option = types.Option

// TextValued is implemented by a type that crosses the wire as a JSON string
// rather than as whatever its Go kind implies. Values lists the strings it
// accepts. Other, when non-empty, is a regular expression admitting strings
// outside that list; it exists for a type that can legitimately produce a value
// the list does not name.
//
// Schema inference reads the Go kind, so a named integer with a MarshalText
// marshals as a string but infers as an integer — a schema that describes
// nothing the capability accepts. A type stating which strings it accepts is a
// fact about the type, not protocol vocabulary, which is why the statement
// belongs here rather than in the broker: the broker cannot special-case a
// provider's type without becoming the knower this extension point exists to
// avoid.
//
// The broker walks every request and response type once, at its own Start,
// collects the types implementing this interface, and renders each as a string
// schema carrying its accepted values.
type TextValued interface {
	encoding.TextMarshaler
	encoding.TextUnmarshaler

	// TextValues reports the strings this type accepts, and optionally a
	// regular expression admitting others.
	TextValues() (values []string, other string)
}
