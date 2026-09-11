package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

type echoCmd kernel.Command[echoRequest, echoResponse]
type echoRequest struct{ Text string }
type echoResponse struct{ Text string }

// bareCmd carries a non-struct request, which no schema root can describe.
type bareCmd kernel.Command[string, echoResponse]

type echoPlugin struct{ seen *echoRequest }

func (p echoPlugin) Name() kernel.PluginName           { return "echo" }
func (p echoPlugin) Dependencies() []kernel.PluginName { return nil }

func (p echoPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[echoCmd](func() (kernel.Lock, kernel.Execute[echoRequest, echoResponse]) {
		return nil, func(_ kernel.Kernel, request echoRequest) (echoResponse, error) {
			*p.seen = request
			return echoResponse{Text: request.Text + "!"}, nil
		}
	})
	return nil
}

func startEngine(t *testing.T, plugins ...kernel.Plugin) *kernel.Engine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	engine := kernel.New(nil).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(plugins...)
	go engine.Run(ctx)
	<-engine.Ready()
	return engine
}

// The broker allocates the request with reflect.New and hands the pointer to
// Invoke; the typed dispatch behind it must see exactly that value.
func TestCommand_InvokePassesRequestThroughUnchanged(t *testing.T) {
	var seen echoRequest
	engine := startEngine(t, echoPlugin{seen: &seen})

	capability := Command[echoCmd, echoRequest, echoResponse]("echo", "echo the text back")
	if err := capability.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}

	request := reflect.New(capability.RequestType())
	request.Elem().Set(reflect.ValueOf(echoRequest{Text: "hello"}))
	response, err := capability.Invoke(engine.Executioner(), request.Interface())
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if seen.Text != "hello" {
		t.Fatalf("dispatch saw %+v, want the request unchanged", seen)
	}
	if response != (echoResponse{Text: "hello!"}) {
		t.Fatalf("response = %#v, want the command's own", response)
	}
}

// Capabilities returns a slice literal, so there is nowhere to return an error:
// a malformed one defers into Err for the broker to collect at Start.
func TestCommand_NonStructRequestDefersIntoErr(t *testing.T) {
	capability := Command[bareCmd, string, echoResponse]("bare", "cannot be described")

	var payload ErrNonStructPayload
	if !errors.As(capability.Err(), &payload) || payload.Role != "request" {
		t.Fatalf("Err() = %v, want ErrNonStructPayload on the request", capability.Err())
	}
}

func TestFunc_NonStructResponseDefersIntoErr(t *testing.T) {
	capability := Func("bare", "cannot be described",
		func(kernel.Executioner, echoRequest) (string, error) { return "", nil })

	var payload ErrNonStructPayload
	if !errors.As(capability.Err(), &payload) || payload.Role != "response" {
		t.Fatalf("Err() = %v, want ErrNonStructPayload on the response", capability.Err())
	}
}

func TestCapability_NameIsValidated(t *testing.T) {
	for _, name := range []string{"", "Capture", "9lives", "with-dash", "with space"} {
		capability := Func(name, "", func(kernel.Executioner, echoRequest) (echoResponse, error) {
			return echoResponse{}, nil
		})
		var invalid ErrInvalidCapabilityName
		if !errors.As(capability.Err(), &invalid) {
			t.Errorf("name %q: Err() = %v, want ErrInvalidCapabilityName", name, capability.Err())
		}
	}
	for _, name := range []string{"a", "capture", "send_input", "step2"} {
		capability := Func(name, "", func(kernel.Executioner, echoRequest) (echoResponse, error) {
			return echoResponse{}, nil
		})
		if capability.Err() != nil {
			t.Errorf("name %q: Err() = %v, want nil", name, capability.Err())
		}
	}
}

func TestReadOnly_SetsTheAnnotationSource(t *testing.T) {
	body := func(kernel.Executioner, echoRequest) (echoResponse, error) { return echoResponse{}, nil }
	if Func("plain", "", body).ReadOnly() {
		t.Fatal("a capability is not read-only unless it says so")
	}
	if !Func("stated", "", body, ReadOnly()).ReadOnly() {
		t.Fatal("ReadOnly() did not reach the capability")
	}
}

func TestUnavailable_IsAnOrdinaryError(t *testing.T) {
	var err error = Unavailable{Reason: "the game is shutting down"}
	if err.Error() != "the game is shutting down" {
		t.Fatalf("Error() = %q", err.Error())
	}
	var unavailable Unavailable
	if !errors.As(err, &unavailable) {
		t.Fatal("Unavailable is not reachable through errors.As")
	}
}
