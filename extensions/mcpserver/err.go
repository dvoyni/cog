package mcpserver

import (
	"fmt"
	"reflect"
)

// ErrListen reports that the broker could not bind its address. It terminates
// the engine rather than leaving the server silently absent, because from the
// agent's side "not offered" and "not running" look identical, and the common
// cause — a stale game process still holding the port — is exactly the case
// where a loud failure saves the most time.
//
// A browser build reaches this error too: net.Listen does not work there, and
// an app that composed the broker into a web build made a composition mistake.
// Excluding the plugin behind a build tag would break a main.go shared between
// desktop and web at compile time, which is a worse failure than the one build
// that is actually wrong failing at startup.
type ErrListen struct {
	Addr string
	Err  error
}

func (e ErrListen) Error() string {
	return fmt.Sprintf("mcpserver: cannot listen on %s: %v", e.Addr, e.Err)
}

func (e ErrListen) Unwrap() error { return e.Err }

// ErrMalformedCapability reports a capability the broker cannot render as a
// tool. It terminates the engine rather than skipping the capability: a
// silently absent tool is close to undebuggable from the agent's side, and this
// is a composition error, which cog fails loudly.
type ErrMalformedCapability struct {
	Provider   string
	Capability string
	Err        error
}

func (e ErrMalformedCapability) Error() string {
	return fmt.Sprintf("mcpserver: provider %q offers malformed capability %q: %v",
		e.Provider, e.Capability, e.Err)
}

func (e ErrMalformedCapability) Unwrap() error { return e.Err }

// ErrDuplicateCapability reports one provider offering the same capability name
// twice. A duplicate across providers cannot happen: the tool name carries the
// plugin prefix, and the engine already rejects duplicate plugin names.
type ErrDuplicateCapability struct {
	Provider   string
	Capability string
}

func (e ErrDuplicateCapability) Error() string {
	return fmt.Sprintf("mcpserver: provider %q offers capability %q twice", e.Provider, e.Capability)
}

// ErrNonObjectSchema reports a payload type whose inferred JSON schema root is
// not an object. The protocol requires an object at the root of a tool's input
// schema, and the SDK panics on anything else, so the check is the broker's
// obligation rather than a nicety.
type ErrNonObjectSchema struct {
	Type reflect.Type
	Root string
}

func (e ErrNonObjectSchema) Error() string {
	return fmt.Sprintf("mcpserver: type %v infers a %q schema root, want \"object\"", e.Type, e.Root)
}
