package mcpserver

import (
	"reflect"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// offered is one capability and the provider that offered it. The provider name
// is all the broker ever learns about where a capability came from.
type offered struct {
	provider   kernel.PluginName
	capability mcp.Capability
}

// toolName renders the protocol name for this capability: <plugin>_<capability>.
// Underscore, because the MCP name charset is conservative and it is the one
// separator no client rejects. Uniqueness across providers is inherited from
// the engine's own rejection of duplicate plugin names.
func (o offered) toolName() string { return string(o.provider) + "_" + o.capability.Name() }

// collect asks every provider for its capabilities, exactly once, and rejects
// the two failures a provider can hand over: a deferred construction error, and
// the same name twice within one provider.
func collect(providers []mcp.Provider) ([]offered, error) {
	all := make([]offered, 0, len(providers))
	for _, provider := range providers {
		seen := map[string]struct{}{}
		for _, capability := range provider.Capabilities() {
			if err := capability.Err(); err != nil {
				return nil, ErrMalformedCapability{
					Provider: string(provider.Name()), Capability: capability.Name(), Err: err,
				}
			}
			if _, duplicate := seen[capability.Name()]; duplicate {
				return nil, ErrDuplicateCapability{
					Provider: string(provider.Name()), Capability: capability.Name(),
				}
			}
			seen[capability.Name()] = struct{}{}
			all = append(all, offered{provider: provider.Name(), capability: capability})
		}
	}
	return all, nil
}

// render turns every collected capability into a tool. Schemas are inferred
// from the Go types, so a provider never writes one and tool-definition drift
// is structural rather than a promise.
func render(all []offered) ([]*sdk.Tool, error) {
	overrides := textSchemas(all)
	tools := make([]*sdk.Tool, 0, len(all))
	for _, one := range all {
		tool, err := renderTool(one, overrides)
		if err != nil {
			return nil, ErrMalformedCapability{
				Provider: string(one.provider), Capability: one.capability.Name(), Err: err,
			}
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func renderTool(one offered, overrides map[reflect.Type]*jsonschema.Schema) (*sdk.Tool, error) {
	input, err := payloadSchema(one.capability.RequestType(), overrides)
	if err != nil {
		return nil, err
	}
	output, err := payloadSchema(one.capability.ResponseType(), overrides)
	if err != nil {
		return nil, err
	}
	return &sdk.Tool{
		Name:         one.toolName(),
		Description:  one.capability.Description(),
		InputSchema:  input,
		OutputSchema: output,
		Annotations: &sdk.ToolAnnotations{
			// ReadOnlyHint is the provider's own statement. DestructiveHint is
			// always false and is never derived from !readOnly: a capability
			// that writes the one file it was asked to write is not
			// destructive. OpenWorldHint is always false, because a running
			// game is a closed world. IdempotentHint is left alone.
			ReadOnlyHint:    one.capability.ReadOnly(),
			DestructiveHint: newFalse(),
			OpenWorldHint:   newFalse(),
		},
	}, nil
}

// payloadSchema infers the schema for one request or response type and rejects
// a root the protocol cannot carry. The SDK panics on a non-object input
// schema, so checking here is an obligation rather than a nicety.
func payloadSchema(
	payload reflect.Type, overrides map[reflect.Type]*jsonschema.Schema,
) (*jsonschema.Schema, error) {
	schema, err := jsonschema.ForType(payload, &jsonschema.ForOptions{TypeSchemas: overrides})
	if err != nil {
		return nil, err
	}
	if schema.Type != "object" {
		return nil, ErrNonObjectSchema{Type: payload, Root: schema.Type}
	}
	return schema, nil
}

var textValuedType = reflect.TypeFor[mcp.TextValued]()

// textSchemas walks every request and response type once and renders each
// mcp.TextValued type it reaches as the string it actually crosses the wire as.
// This is the only place the broker inspects a provider's types for anything
// beyond their shape, and what it learns is a string set, never a meaning.
func textSchemas(all []offered) map[reflect.Type]*jsonschema.Schema {
	overrides := map[reflect.Type]*jsonschema.Schema{}
	seen := map[reflect.Type]struct{}{}
	for _, one := range all {
		walkTextValued(one.capability.RequestType(), seen, overrides)
		walkTextValued(one.capability.ResponseType(), seen, overrides)
	}
	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

// walkTextValued descends through pointers, slices, arrays, maps and struct
// fields, so a type is found wherever it is nested rather than only at the top
// level.
func walkTextValued(
	payload reflect.Type, seen map[reflect.Type]struct{}, overrides map[reflect.Type]*jsonschema.Schema,
) {
	if payload == nil {
		return
	}
	if _, visited := seen[payload]; visited {
		return
	}
	seen[payload] = struct{}{}

	if schema, ok := textSchema(payload); ok {
		// A type that crosses as text is a leaf: whatever it holds in Go is not
		// what the agent sends.
		overrides[payload] = schema
		overrides[reflect.PointerTo(payload)] = schema
		return
	}

	switch payload.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		walkTextValued(payload.Elem(), seen, overrides)
	case reflect.Map:
		walkTextValued(payload.Key(), seen, overrides)
		walkTextValued(payload.Elem(), seen, overrides)
	case reflect.Struct:
		for i := range payload.NumField() {
			if field := payload.Field(i); field.IsExported() {
				walkTextValued(field.Type, seen, overrides)
			}
		}
	}
}

// textSchema renders one mcp.TextValued type: the strings it accepts, plus a
// pattern branch when it can legitimately produce a value the list does not
// name. The enum is what makes a mistyped value fail in the agent's client
// rather than reaching the engine and coming back as an Unavailable.
func textSchema(payload reflect.Type) (*jsonschema.Schema, bool) {
	base := payload
	if base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	// The pointer method set is the superset, and a fresh pointer is always safe
	// to call through, so one check covers a type whose TextUnmarshaler takes a
	// pointer receiver — which every practical implementor's does.
	if base.Kind() == reflect.Invalid || !reflect.PointerTo(base).Implements(textValuedType) {
		return nil, false
	}
	values, other := reflect.New(base).Interface().(mcp.TextValued).TextValues()

	listed := &jsonschema.Schema{Type: "string"}
	for _, value := range values {
		listed.Enum = append(listed.Enum, value)
	}
	if other == "" {
		return listed, true
	}
	loose := &jsonschema.Schema{Type: "string", Pattern: other}
	if len(listed.Enum) == 0 {
		return loose, true
	}
	return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{listed, loose}}, true
}

// newFalse returns a pointer to false. The SDK spells the two hints the broker
// always denies as *bool, where nil means "unstated" and carries the opposite
// default.
func newFalse() *bool {
	value := false
	return &value
}
