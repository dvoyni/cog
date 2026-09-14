package archtest

import (
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/kernel/archtest/internal"
	"github.com/dvoyni/cog/libs/m"
)

// taggedBox's type argument is an unnamed struct whose tag looks like a
// qualified name. A tag is a string, not a type, and TypeName leaves it alone.
type taggedBox = m.Maybe[struct {
	A int `doc:"see bundles/canvas/internal/types.OpQueue"`
}]

// The fixture package's import path is github.com/dvoyni/cog/kernel/archtest/internal,
// so each of its types renders under archtest, the segment before internal.
func TestTypeName_RendersInternalDeclarationsUnderTheirEnclosingPackage(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{"plain named type", reflect.TypeFor[kernel.PluginName](), "kernel.PluginName"},
		{"internal named type", reflect.TypeFor[internal.Font](), "archtest.Font"},
		{"pointer", reflect.TypeFor[*internal.Font](), "*archtest.Font"},
		{"slice of pointers", reflect.TypeFor[[]*internal.Frame](), "[]*archtest.Frame"},
		{"array", reflect.TypeFor[[4]internal.Frame](), "[4]archtest.Frame"},
		{"map", reflect.TypeFor[map[internal.Font][]internal.Frame](), "map[archtest.Font][]archtest.Frame"},
		{"channel", reflect.TypeFor[chan internal.Frame](), "chan archtest.Frame"},
		{"receive channel", reflect.TypeFor[<-chan internal.Frame](), "<-chan archtest.Frame"},
		{"send channel", reflect.TypeFor[chan<- internal.Frame](), "chan<- archtest.Frame"},
		{"channel of receive channels", reflect.TypeFor[chan (<-chan internal.Frame)](), "chan (<-chan archtest.Frame)"},
		{"func", reflect.TypeFor[func(internal.Font, ...*internal.Frame) (internal.Font, error)](),
			"func(archtest.Font, ...*archtest.Frame) (archtest.Font, error)"},
		{"func with one result", reflect.TypeFor[func() internal.Font](), "func() archtest.Font"},
		{"func with nothing", reflect.TypeFor[func()](), "func()"},
		{"generic with an internal argument", reflect.TypeFor[m.Maybe[*internal.Font]](), "m.Maybe[*archtest.Font]"},
		{"internal generic", reflect.TypeFor[internal.Box[internal.Font]](), "archtest.Box[archtest.Font]"},
		{"nested generic arguments", reflect.TypeFor[m.Maybe[internal.Box[map[string]*internal.Frame]]](),
			"m.Maybe[archtest.Box[map[string]*archtest.Frame]]"},
		{"kernel generic", reflect.TypeFor[kernel.Read[*internal.Font]](), "kernel.Read[*archtest.Font]"},
		{"stdlib type", reflect.TypeFor[time.Duration](), "time.Duration"},
		{"stdlib generic with a stdlib argument", reflect.TypeFor[atomic.Pointer[http.Request]](), "atomic.Pointer[http.Request]"},
		{"struct tag in a type argument", reflect.TypeFor[taggedBox](),
			`m.Maybe[struct { A int "doc:\"see bundles/canvas/internal/types.OpQueue\"" }]`},
		{"predeclared type", reflect.TypeFor[int](), "int"},
		{"predeclared interface", reflect.TypeFor[error](), "error"},
		{"unnamed struct", reflect.TypeFor[struct{ Font internal.Font }](), "struct { Font internal.Font }"},
		{"nil", nil, "<nil>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := kernel.TypeName(tc.typ); got != tc.want {
				t.Errorf("TypeName = %q, want %q", got, tc.want)
			}
		})
	}
}
