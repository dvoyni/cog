package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/google/jsonschema-go/jsonschema"
)

// testKey is an integer that crosses the wire as text — the shape schema
// inference gets wrong, because it reads the Go kind and would call this an
// integer.
type testKey int

var testKeyNames = map[testKey]string{1: "enter", 2: "escape"}

func (k testKey) MarshalText() ([]byte, error) {
	if name, ok := testKeyNames[k]; ok {
		return []byte(name), nil
	}
	return fmt.Appendf(nil, "#%d", int(k)), nil
}

func (k *testKey) UnmarshalText(text []byte) error {
	for value, name := range testKeyNames {
		if name == string(text) {
			*k = value
			return nil
		}
	}
	number, err := strconv.Atoi(strings.TrimPrefix(string(text), "#"))
	if err != nil {
		return err
	}
	*k = testKey(number)
	return nil
}

func (testKey) TextValues() ([]string, string) {
	return []string{"enter", "escape"}, "^#-?[0-9]+$"
}

// plainKey accepts nothing outside its list, so its schema is a bare enum.
type plainKey int

func (k plainKey) MarshalText() ([]byte, error) { return []byte("only"), nil }
func (k *plainKey) UnmarshalText([]byte) error  { *k = 0; return nil }
func (plainKey) TextValues() ([]string, string) { return []string{"only"}, "" }

type testAction struct {
	Key testKey `json:"key"`
}

// nestedRequest reaches testKey only through a slice of structs, which is where
// a top-level-only walk would miss it.
type nestedRequest struct {
	Actions  []testAction `json:"actions"`
	Fallback plainKey     `json:"fallback"`
}

func renderOne(t *testing.T, capability mcp.Capability) *jsonschema.Schema {
	t.Helper()
	tools, err := render([]offered{{provider: "probe", capability: capability}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	schema, ok := tools[0].InputSchema.(*jsonschema.Schema)
	if !ok {
		t.Fatalf("input schema is %T, want *jsonschema.Schema", tools[0].InputSchema)
	}
	return schema
}

// A type that crosses the wire as text is rendered as a string schema with its
// accepted values, rather than by its Go kind — and the walk finds it nested
// inside a slice of structs, not only at the top level.
func TestRender_TextValuedTypeNestedInASliceOfStructs(t *testing.T) {
	capability := mcp.Func("send", "", func(kernel.Executioner, nestedRequest) (echoResponse, error) {
		return echoResponse{}, nil
	})
	schema := renderOne(t, capability)

	key := schema.Properties["actions"].Items.Properties["key"]
	if len(key.AnyOf) != 2 {
		t.Fatalf("nested key schema = %+v, want an anyOf of the enum and the pattern", key)
	}
	listed, loose := key.AnyOf[0], key.AnyOf[1]
	if listed.Type != "string" || len(listed.Enum) != 2 || listed.Enum[0] != "enter" {
		t.Fatalf("enum branch = %+v, want the accepted values as strings", listed)
	}
	if loose.Type != "string" || loose.Pattern != "^#-?[0-9]+$" {
		t.Fatalf("pattern branch = %+v, want the other-values expression", loose)
	}

	fallback := schema.Properties["fallback"]
	if fallback.Type != "string" || len(fallback.Enum) != 1 || len(fallback.AnyOf) != 0 {
		t.Fatalf("fallback schema = %+v, want a bare string enum", fallback)
	}
}

// Without the override the same field would infer as an integer, which
// describes nothing the capability accepts.
func TestRender_WithoutTextValuedTheKindWouldWin(t *testing.T) {
	schema, err := jsonschema.ForType(reflect.TypeFor[nestedRequest](), nil)
	if err != nil {
		t.Fatalf("ForType: %v", err)
	}
	if got := schema.Properties["actions"].Items.Properties["key"].Type; got != "integer" {
		t.Fatalf("unoverridden key type = %q, want the Go kind %q", got, "integer")
	}
}

func TestRender_ToolNameIsPluginThenCapability(t *testing.T) {
	tools, err := render([]offered{{provider: "probe", capability: echoing("echo")}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if tools[0].Name != "probe_echo" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "probe_echo")
	}
	if tools[0].Description != "echo the text back" {
		t.Fatalf("description = %q, want the provider's prose", tools[0].Description)
	}
}

// A struct is not enough on its own: time.Time is a struct that marshals as a
// string, so its schema root is not an object. The SDK panics on such an input
// schema, which makes the check the broker's obligation rather than a nicety.
func TestRender_NonObjectSchemaRootIsRejected(t *testing.T) {
	capability := mcp.Func("stamp", "", func(kernel.Executioner, time.Time) (echoResponse, error) {
		return echoResponse{}, nil
	})
	if err := capability.Err(); err != nil {
		t.Fatalf("time.Time is a struct, so construction should succeed: %v", err)
	}

	_, err := render([]offered{{provider: "probe", capability: capability}})
	var root mcp.ErrNonObjectSchema
	if !errors.As(err, &root) || root.Root == "object" {
		t.Fatalf("render error = %v, want ErrNonObjectSchema", err)
	}
}

type testRect struct {
	X     float32 `json:"x"`
	Width float32 `json:"width"`
}

// pointerOptionals and maybeOptionals are one request spelled both ways an
// optional field can be: the schema an agent reads must not say which.
type pointerOptionals struct {
	Layer *int      `json:"layer,omitempty" jsonschema:"a layer bound"`
	Rect  *testRect `json:"rect,omitempty"`
	Many  []*int    `json:"many,omitempty"`
}

type maybeOptionals struct {
	Layer m.Maybe[int]      `json:"layer,omitzero" jsonschema:"a layer bound"`
	Rect  m.Maybe[testRect] `json:"rect,omitzero"`
	Many  []m.Maybe[int]    `json:"many,omitzero"`
}

// A Maybe crosses the wire as the nullable value a pointer did, so it renders
// as the pointer's schema - never as the struct of unexported fields it is in
// Go.
func TestRender_AMaybeRendersAsThePointerItReplaced(t *testing.T) {
	pointer := renderOne(t, mcp.Func("pointer", "", func(kernel.Executioner, pointerOptionals) (echoResponse, error) {
		return echoResponse{}, nil
	}))
	maybe := renderOne(t, mcp.Func("maybe", "", func(kernel.Executioner, maybeOptionals) (echoResponse, error) {
		return echoResponse{}, nil
	}))
	want, err := json.Marshal(pointer)
	if err != nil {
		t.Fatalf("marshal pointer schema: %v", err)
	}
	got, err := json.Marshal(maybe)
	if err != nil {
		t.Fatalf("marshal maybe schema: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("maybe schema = %s\nwant the pointer's %s", got, want)
	}
}

func renderOutput(t *testing.T, capability mcp.Capability) *jsonschema.Schema {
	t.Helper()
	tools, err := render([]offered{{provider: "probe", capability: capability}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	schema, ok := tools[0].OutputSchema.(*jsonschema.Schema)
	if !ok {
		t.Fatalf("output schema is %T, want *jsonschema.Schema", tools[0].OutputSchema)
	}
	return schema
}

// sliceLists and listLists are one response spelled with slices and with
// m.List: a List crosses the wire as the array its MarshalJSON writes, so the
// schema an agent reads must describe that array and never the struct of
// unexported fields it is in Go.
type sliceLists struct {
	Rects  []testRect   `json:"rects"`
	Nested [][]testRect `json:"nested"`
	Keys   []testAction `json:"keys"`
	Maybes []*int       `json:"maybes"`
}

type listLists struct {
	Rects  m.List[testRect]         `json:"rects"`
	Nested m.List[m.List[testRect]] `json:"nested"`
	Keys   m.List[testAction]       `json:"keys"`
	Maybes m.List[m.Maybe[int]]     `json:"maybes"`
}

// A List renders as the array of its element's schema, nested Lists as arrays
// of arrays, and never as an object. The array is not nullable: an empty List
// marshals as [], never null.
func TestRender_AListRendersAsTheArrayItCrossesAs(t *testing.T) {
	schema := renderOutput(t, mcp.Func("list", "", func(kernel.Executioner, echoResponse) (listLists, error) {
		return listLists{}, nil
	}))

	rects := schema.Properties["rects"]
	if rects.Type != "array" || len(rects.Types) != 0 {
		t.Fatalf("rects schema type = %q %v, want a non-nullable array", rects.Type, rects.Types)
	}
	if rects.Items == nil || rects.Items.Type != "object" || rects.Items.Properties["width"] == nil {
		t.Fatalf("rects items = %+v, want testRect's object schema", rects.Items)
	}

	nested := schema.Properties["nested"]
	if nested.Type != "array" || len(nested.Types) != 0 || nested.Items == nil {
		t.Fatalf("nested schema = %+v, want a non-nullable array", nested)
	}
	inner := nested.Items
	if inner.Type != "array" || len(inner.Types) != 0 || inner.Items == nil || inner.Items.Properties["x"] == nil {
		t.Fatalf("nested items = %+v, want a non-nullable array of testRect", inner)
	}
}

// The element is walked before the List renders, so a TextValued type inside
// it renders as its string enum and a Maybe inside it as its nullable value.
func TestRender_AListsElementKeepsItsOwnOverrides(t *testing.T) {
	list := renderOutput(t, mcp.Func("list", "", func(kernel.Executioner, echoResponse) (listLists, error) {
		return listLists{}, nil
	}))
	slice := renderOutput(t, mcp.Func("slice", "", func(kernel.Executioner, echoResponse) (sliceLists, error) {
		return sliceLists{}, nil
	}))

	key := list.Properties["keys"].Items.Properties["key"]
	if len(key.AnyOf) != 2 || key.AnyOf[0].Type != "string" || len(key.AnyOf[0].Enum) != 2 {
		t.Fatalf("key inside a List = %+v, want the TextValued enum and pattern", key)
	}

	want, err := json.Marshal(slice.Properties["maybes"].Items)
	if err != nil {
		t.Fatalf("marshal slice item: %v", err)
	}
	got, err := json.Marshal(list.Properties["maybes"].Items)
	if err != nil {
		t.Fatalf("marshal list item: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("Maybe inside a List = %s\nwant the pointer's %s", got, want)
	}
}
