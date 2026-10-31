package internal

import (
	"reflect"
	"strings"

	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// offered is one capability and the plugin that contributed its provider. The
// plugin name is all the broker ever learns about where a capability came from.
type offered struct {
	provider   kernel.PluginName
	capability mcp.Capability
}

// toolName renders the protocol name for this capability: <plugin>_<capability>.
// Underscore, because the MCP name charset is conservative and it is the one
// separator no client rejects. Uniqueness across plugins is inherited from the
// engine's own rejection of duplicate plugin names.
func (o offered) toolName() string { return string(o.provider) + "_" + o.capability.Name() }

// collect asks every provider for its capabilities, exactly once, and rejects
// the two failures a provider can hand over: a deferred construction error, and
// the same name twice from one plugin. A plugin may contribute more than one
// Provider, and every tool name they render shares its prefix, so names are
// unique per plugin rather than per Provider.
func collect(providers []kernel.ContributedAdapter[mcp.Provider]) ([]offered, error) {
	all := make([]offered, 0, len(providers))
	seen := map[kernel.PluginName]map[string]struct{}{}
	for _, contributed := range providers {
		names := seen[contributed.Plugin]
		if names == nil {
			names = map[string]struct{}{}
			seen[contributed.Plugin] = names
		}
		for _, capability := range contributed.Adapter.Capabilities() {
			if err := capability.Err(); err != nil {
				return nil, mcp.ErrMalformedCapability{
					Provider: string(contributed.Plugin), Capability: capability.Name(), Err: err,
				}
			}
			if _, duplicate := names[capability.Name()]; duplicate {
				return nil, mcp.ErrDuplicateCapability{
					Provider: string(contributed.Plugin), Capability: capability.Name(),
				}
			}
			names[capability.Name()] = struct{}{}
			all = append(all, offered{provider: contributed.Plugin, capability: capability})
		}
	}
	return all, nil
}

// render turns every collected capability into a tool. Schemas are inferred
// from the Go types, so a provider never writes one and tool-definition drift
// is structural rather than a promise.
func render(all []offered) ([]*sdk.Tool, error) {
	overrides, err := typeSchemas(all)
	if err != nil {
		return nil, err
	}
	tools := make([]*sdk.Tool, 0, len(all))
	for _, one := range all {
		tool, err := renderTool(one, overrides)
		if err != nil {
			return nil, mcp.ErrMalformedCapability{
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
		return nil, mcp.ErrNonObjectSchema{Type: payload, Root: schema.Type}
	}
	return schema, nil
}

var textValuedType = reflect.TypeFor[mcp.TextValued]()

// maybeType is one instantiation of m.Maybe, read for the package and the
// generic name every other instantiation shares.
var maybeType = reflect.TypeFor[m.Maybe[struct{}]]()

// listType is one instantiation of m.List, read the same way as maybeType.
var listType = reflect.TypeFor[m.List[struct{}]]()

// typeSchemas walks every request and response type once and renders the three
// kinds of type whose wire form is not their Go shape: each mcp.TextValued type
// as the string it actually crosses the wire as, each m.Maybe as the nullable
// value it crosses as, and each m.List as the array it crosses as. This is the only place the broker inspects a
// provider's types for anything beyond their shape, and what it learns is a
// string set or an element type, never a meaning.
func typeSchemas(all []offered) (map[reflect.Type]*jsonschema.Schema, error) {
	overrides := map[reflect.Type]*jsonschema.Schema{}
	seen := map[reflect.Type]struct{}{}
	for _, one := range all {
		for _, payload := range []reflect.Type{one.capability.RequestType(), one.capability.ResponseType()} {
			if err := walkOverrides(payload, seen, overrides); err != nil {
				return nil, mcp.ErrMalformedCapability{
					Provider: string(one.provider), Capability: one.capability.Name(), Err: err,
				}
			}
		}
	}
	if len(overrides) == 0 {
		return nil, nil
	}
	return overrides, nil
}

// walkOverrides descends through pointers, slices, arrays, maps and struct
// fields, so a type is found wherever it is nested rather than only at the top
// level.
func walkOverrides(
	payload reflect.Type, seen map[reflect.Type]struct{}, overrides map[reflect.Type]*jsonschema.Schema,
) error {
	if payload == nil {
		return nil
	}
	if _, visited := seen[payload]; visited {
		return nil
	}
	seen[payload] = struct{}{}

	if schema, ok := textSchema(payload); ok {
		// A type that crosses as text is a leaf: whatever it holds in Go is not
		// what the agent sends.
		overrides[payload] = schema
		overrides[reflect.PointerTo(payload)] = schema
		return nil
	}

	if element, ok := maybeElement(payload); ok {
		// A Maybe's value is an unexported field the walk below would not
		// reach, so its element is walked first and the Maybe then renders as
		// a pointer to it would: the element's schema with null admitted.
		if err := walkOverrides(element, seen, overrides); err != nil {
			return err
		}
		schema, err := jsonschema.ForType(reflect.PointerTo(element), &jsonschema.ForOptions{TypeSchemas: overrides})
		if err != nil {
			return err
		}
		overrides[payload] = schema
		overrides[reflect.PointerTo(payload)] = schema
		return nil
	}

	if element, ok := listElement(payload); ok {
		// A List's elements sit in an unexported slice the walk below would not
		// reach, so its element is walked first and the List then renders as
		// the slice it marshals as. MarshalJSON writes [] for an empty List and
		// never null, so the array is not nullable.
		if err := walkOverrides(element, seen, overrides); err != nil {
			return err
		}
		schema, err := jsonschema.ForType(reflect.SliceOf(element), &jsonschema.ForOptions{TypeSchemas: overrides})
		if err != nil {
			return err
		}
		schema.Type, schema.Types = "array", nil
		overrides[payload] = schema
		overrides[reflect.PointerTo(payload)] = schema
		return nil
	}

	switch payload.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return walkOverrides(payload.Elem(), seen, overrides)
	case reflect.Map:
		if err := walkOverrides(payload.Key(), seen, overrides); err != nil {
			return err
		}
		return walkOverrides(payload.Elem(), seen, overrides)
	case reflect.Struct:
		for i := range payload.NumField() {
			if field := payload.Field(i); field.IsExported() {
				if err := walkOverrides(field.Type, seen, overrides); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// maybeElement reports whether payload is an instantiation of m.Maybe, and the
// type it holds. reflect has no generic origin to compare, so an instantiation
// is recognised by m's package and the name every one of them starts with.
func maybeElement(payload reflect.Type) (reflect.Type, bool) {
	if payload.Kind() != reflect.Struct || payload.PkgPath() != maybeType.PkgPath() {
		return nil, false
	}
	generic := maybeType.Name()[:strings.IndexByte(maybeType.Name(), '[')+1]
	if !strings.HasPrefix(payload.Name(), generic) {
		return nil, false
	}
	get, ok := payload.MethodByName("Get")
	if !ok {
		return nil, false
	}
	return get.Type.Out(0), true
}

// listElement reports whether payload is an instantiation of m.List, and the
// type it holds, recognised as maybeElement recognises a Maybe. The element is
// the result of At.
func listElement(payload reflect.Type) (reflect.Type, bool) {
	if payload.Kind() != reflect.Struct || payload.PkgPath() != listType.PkgPath() {
		return nil, false
	}
	generic := listType.Name()[:strings.IndexByte(listType.Name(), '[')+1]
	if !strings.HasPrefix(payload.Name(), generic) {
		return nil, false
	}
	at, ok := payload.MethodByName("At")
	if !ok {
		return nil, false
	}
	return at.Type.Out(0), true
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
