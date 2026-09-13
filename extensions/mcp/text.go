package mcp

import "encoding"

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
