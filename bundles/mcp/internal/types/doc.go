// Package types declares the concrete types mcp's root aliases: Capability,
// whose unexported fields only its two constructors, Command and Func, may set,
// and Option, which writes to capability settings no other package can reach.
// It also holds the deferred construction failures those constructors record,
// ErrInvalidCapabilityName and ErrNonStructPayload.
//
// Each is declared here and aliased in the root (type Capability =
// types.Capability). Capability stays a struct rather than an interface because
// that is what makes the capability-body rule unforgeable: no package can
// implement it and slip work onto the broker's goroutine. Its exported methods
// are what the broker reads it through. Go allows nothing outside bundles/mcp to
// import this package, and nothing declared here imports the root, which keeps
// the arrangement acyclic.
package types
