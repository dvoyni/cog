package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

type archCounter int

type archInnerCmd kernel.Command[archInnerRequest, archInnerResponse]
type archInnerRequest struct{}
type archInnerResponse struct{}

type archOuterCmd kernel.Command[archOuterRequest, archOuterResponse]
type archOuterRequest struct{}
type archOuterResponse struct{}

// archInnerCmdImpl is the only handler that names the counter.
func archInnerCmdImpl() (kernel.Lock, kernel.Execute[archInnerRequest, archInnerResponse]) {
	var counter kernel.Write[archCounter]
	return func(access kernel.ResourceAccess) {
			counter = access.GetWrite[archCounter]()
		}, func(kernel.Kernel, archInnerRequest) (archInnerResponse, error) {
			_ = counter.Get()
			return archInnerResponse{}, nil
		}
}

// archOuterCmdImpl names the command, never the resource behind it.
func archOuterCmdImpl() (kernel.Lock, kernel.Execute[archOuterRequest, archOuterResponse]) {
	var inner func(kernel.Kernel, archInnerRequest) (archInnerResponse, error)
	return func(access kernel.ResourceAccess) {
			inner = access.Uses[archInnerCmd]()
		}, func(k kernel.Kernel, _ archOuterRequest) (archOuterResponse, error) {
			_, err := inner(k, archInnerRequest{})
			return archOuterResponse{}, err
		}
}

type archPlugin struct{}

func (archPlugin) Name() kernel.PluginName           { return "arch" }
func (archPlugin) Dependencies() []kernel.PluginName { return nil }

func (archPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(archCounter(0))
	registrar.HandleCommand[archInnerCmd](archInnerCmdImpl)
	registrar.HandleCommand[archOuterCmd](archOuterCmdImpl)
	return nil
}

func describeEngine(t *testing.T, broker *Plugin, arguments string) ArchitectureResponse {
	t.Helper()
	result := callTool(t, broker, broker.Capabilities()[0], arguments)
	if result.IsError {
		t.Fatalf("mcpserver_architecture refused: %s", resultText(t, result))
	}
	var document ArchitectureResponse
	if err := json.Unmarshal([]byte(resultText(t, result)), &document); err != nil {
		t.Fatalf("decode the answer: %v", err)
	}
	return document
}

// The description ships four flat arrays, and the part that earns the tool is
// the resolved lock closure: a write the outer command never named.
func TestArchitecture_ReportsTheResolvedLockClosure(t *testing.T) {
	broker := testBroker()
	runEngine(t, archPlugin{}, broker)

	document := describeEngine(t, broker, `{}`)
	if len(document.Plugins) != 2 || len(document.Resources) != 1 || len(document.Commands) != 2 {
		t.Fatalf("document = %+v, want the four arrays filled", document)
	}
	if document.Resources[0].Type != "mcpserver.archCounter" || document.Resources[0].Owner != "arch" {
		t.Fatalf("resource = %+v, want the type string as its address", document.Resources[0])
	}

	var outer ArchitectureCommand
	for _, command := range document.Commands {
		if command.Type == "mcpserver.archOuterCmd" {
			outer = command
		}
	}
	if len(outer.Writes) != 1 || outer.Writes[0] != "mcpserver.archCounter" {
		t.Fatalf("outer writes = %v, want the lock it never named", outer.Writes)
	}
	if len(outer.Uses) != 1 || outer.Uses[0] != "mcpserver.archInnerCmd" {
		t.Fatalf("outer uses = %v, want the edge that explains the write", outer.Uses)
	}
}

// Given a path, the tool writes the document and answers with the path alone.
func TestArchitecture_WritesAFileWhenGivenAPath(t *testing.T) {
	broker := testBroker()
	runEngine(t, archPlugin{}, broker)

	path := filepath.Join(t.TempDir(), "nested", "architecture.json")
	arguments, err := json.Marshal(ArchitectureRequest{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	document := describeEngine(t, broker, string(arguments))
	if document.Path != path || len(document.Commands) != 0 {
		t.Fatalf("document = %+v, want the path alone", document)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the tool reported a path it did not write: %v", err)
	}
	var onDisk ArchitectureResponse
	if err := json.Unmarshal(written, &onDisk); err != nil {
		t.Fatalf("the written file is not the document: %v", err)
	}
	if len(onDisk.Commands) != 2 {
		t.Fatalf("written document = %+v, want the four arrays", onDisk)
	}

	// Re-writing the same name is the iterate-and-look loop, so an existing
	// file is overwritten without complaint.
	if document := describeEngine(t, broker, string(arguments)); document.Path != path {
		t.Fatalf("second write = %+v, want the same path", document)
	}
}

// Every check happens before anything is written, and each refusal is words
// rather than a fault.
func TestArchitecture_RejectsAPathItCannotHonour(t *testing.T) {
	broker := testBroker()
	reported := runEngine(t, archPlugin{}, broker)

	for name, request := range map[string]ArchitectureRequest{
		"relative":        {Path: filepath.Join("relative", "architecture.json")},
		"wrong extension": {Path: filepath.Join(t.TempDir(), "architecture.txt")},
	} {
		t.Run(name, func(t *testing.T) {
			arguments, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			result := callTool(t, broker, broker.Capabilities()[0], string(arguments))
			if !result.IsError || !strings.Contains(resultText(t, result), request.Path) {
				t.Fatalf("result = %q (error %v), want a refusal naming the path",
					resultText(t, result), result.IsError)
			}
		})
	}
	select {
	case err := <-reported:
		t.Fatalf("a bad path was reported as a fault: %v", err)
	default:
	}
}
