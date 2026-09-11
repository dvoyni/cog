package mcp

import (
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// Capability is a named, described, typed unit of engine functionality a
// Provider offers to an agent. It is fixed for the engine lifetime.
//
// It is a struct with unexported fields rather than an interface, and only
// Command and Func may construct one. That is what makes the capability-body
// rule unforgeable: no other package can implement Capability and slip work
// onto the broker's goroutine.
//
// Construction failures defer into err rather than panicking, because
// Capabilities returns a slice literal and has nowhere to return an error. The
// broker collects the failures at Start and fails composition with them.
type Capability struct {
	name        string
	description string
	request     reflect.Type
	response    reflect.Type
	invoke      func(kernel.Executioner, any) (any, error)
	readOnly    bool
	err         error
}

// Command is the whole capability: dispatch this command with the agent's
// request. It carries zero glue — the provider already has a typed command, and
// the capability is simply the statement that an agent may dispatch it — and it
// satisfies the capability-body rule by construction.
func Command[
	TCommand kernel.CommandConstraint[TRequest, TResponse], TRequest any, TResponse any,
](name, description string, opts ...Option) Capability {
	return newCapability[TRequest, TResponse](name, description, opts,
		func(k kernel.Executioner, request any) (any, error) {
			response, err := k.ExecuteCommand[TCommand, TRequest, TResponse](*request.(*TRequest))
			if err != nil {
				return nil, err
			}
			return response, nil
		})
}

// Func is the escape for a capability that is more than one dispatch: a body
// that must arm something and then wait for a frame cannot be a single command,
// because blocking inside a command handler holds the lock set against the very
// work it waits for.
//
// The body still obeys the capability-body rule: it may dispatch and it may
// wait, and it may not touch provider state.
func Func[TRequest any, TResponse any](
	name, description string,
	invoke func(kernel.Executioner, TRequest) (TResponse, error),
	opts ...Option,
) Capability {
	return newCapability[TRequest, TResponse](name, description, opts,
		func(k kernel.Executioner, request any) (any, error) {
			response, err := invoke(k, *request.(*TRequest))
			if err != nil {
				return nil, err
			}
			return response, nil
		})
}

// newCapability validates the parts both constructors share and erases the
// typed body behind an any holding *TRequest.
func newCapability[TRequest any, TResponse any](
	name, description string, opts []Option, invoke func(kernel.Executioner, any) (any, error),
) Capability {
	settings := capabilitySettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	request, response := reflect.TypeFor[TRequest](), reflect.TypeFor[TResponse]()
	return Capability{
		name:        name,
		description: description,
		request:     request,
		response:    response,
		invoke:      invoke,
		readOnly:    settings.readOnly,
		err:         validate(name, request, response),
	}
}

// validate reports the first construction failure, or nil. Both payload types
// must be structs because a JSON schema root has to be an object, so a
// one-value answer is a one-field struct.
func validate(name string, request, response reflect.Type) error {
	if !validName(name) {
		return ErrInvalidCapabilityName{Name: name}
	}
	if request.Kind() != reflect.Struct {
		return ErrNonStructPayload{Capability: name, Role: "request", Type: request}
	}
	if response.Kind() != reflect.Struct {
		return ErrNonStructPayload{Capability: name, Role: "response", Type: response}
	}
	return nil
}

// validName reports whether name matches ^[a-z][a-z0-9_]*$. The broker renders
// the tool name as <plugin>_<capability>, and the MCP name charset is
// conservative enough that nothing wider is worth accepting.
func validName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// Name reports the capability name, unqualified by its provider.
func (c Capability) Name() string { return c.name }

// Description reports the prose an agent reads to decide whether to call this.
func (c Capability) Description() string { return c.description }

// RequestType reports the struct type the agent's arguments unmarshal into.
func (c Capability) RequestType() reflect.Type { return c.request }

// ResponseType reports the struct type the capability answers with.
func (c Capability) ResponseType() reflect.Type { return c.response }

// ReadOnly reports whether this capability changes the game. It is cog's
// reading of the hint, and looser than the hint's own wording: a capture writes
// exactly one file it was told to write and is still read-only here.
func (c Capability) ReadOnly() bool { return c.readOnly }

// Err reports the deferred construction failure, if any. The broker collects
// these at Start; a capability carrying one never reaches an agent.
func (c Capability) Err() error { return c.err }

// Invoke runs the capability. request is an any holding a *TRequest, which the
// broker allocates with reflect.New(c.RequestType()) and unmarshals into; the
// returned any holds the response struct.
//
// k is already bound to the agent's request context, so the dispatch dies on
// either the agent hanging up or engine shutdown, with no bookkeeping here.
func (c Capability) Invoke(k kernel.Executioner, request any) (any, error) {
	return c.invoke(k, request)
}
