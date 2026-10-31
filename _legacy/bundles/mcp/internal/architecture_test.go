package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/mcp"
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
		}, func(kernel.Kernel, archInnerRequest) archInnerResponse {
			_ = counter.Get()
			return archInnerResponse{}
		}
}

// archOuterCmdImpl names the command, never the resource behind it.
func archOuterCmdImpl() (kernel.Lock, kernel.Execute[archOuterRequest, archOuterResponse]) {
	var inner func(kernel.Kernel, archInnerRequest) archInnerResponse
	return func(access kernel.ResourceAccess) {
			inner = access.Uses[archInnerCmd]()
		}, func(k kernel.Kernel, _ archOuterRequest) archOuterResponse {
			inner(k, archInnerRequest{})
			return archOuterResponse{}
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

func describeEngine(t *testing.T, broker *plugin, arguments string) architectureResponse {
	t.Helper()
	result := callTool(t, broker, provider{}.Capabilities()[0], arguments)
	if result.IsError {
		t.Fatalf("mcpserver_architecture refused: %s", resultText(t, result))
	}
	var document architectureResponse
	if err := json.Unmarshal([]byte(resultText(t, result)), &document); err != nil {
		t.Fatalf("decode the answer: %v", err)
	}
	return document
}

// The description ships five flat arrays beside the contention report, and the
// part that earns the tool is the resolved lock closure: a write the outer
// command never named.
func TestArchitecture_ReportsTheResolvedLockClosure(t *testing.T) {
	broker := testBroker()
	runEngine(t, archPlugin{}, broker)

	document := describeEngine(t, broker, `{}`)
	if len(document.Plugins) != 2 || len(document.Resources) != 1 || len(document.Ports) != 1 ||
		len(document.Commands) != 2 {
		t.Fatalf("document = %+v, want the arrays filled", document)
	}
	if document.Resources[0].Type != "mcp.archCounter" || document.Resources[0].Owner != "arch" {
		t.Fatalf("resource = %+v, want the type string as its address", document.Resources[0])
	}

	var outer architectureCommand
	for _, command := range document.Commands {
		if command.Type == "mcp.archOuterCmd" {
			outer = command
		}
	}
	if len(outer.Writes) != 1 || outer.Writes[0] != "mcp.archCounter" {
		t.Fatalf("outer writes = %v, want the lock it never named", outer.Writes)
	}
	if len(outer.Uses) != 1 || outer.Uses[0] != "mcp.archInnerCmd" {
		t.Fatalf("outer uses = %v, want the edge that explains the write", outer.Uses)
	}
}

// The Ports array is the kernel's, named by Port and Adapter types: the broker
// collects mcp.ProviderPort and its own mcp.McpProvider is among the
// contributors, beside every other Adapter provided for it.
func TestArchitecture_ReportsEveryPortAndItsContributors(t *testing.T) {
	broker := testBroker()
	provider := &testProvider{name: "probe", capabilities: []mcp.Capability{echoing("echo")}}
	runEngine(t, archPlugin{}, provider, broker)

	document := describeEngine(t, broker, `{}`)
	if len(document.Ports) != 1 {
		t.Fatalf("ports = %+v, want the broker's one declaration", document.Ports)
	}
	port := document.Ports[0]
	if port.Interface != "mcp.Provider" || port.Port != "mcp.ProviderPort" || !port.Collects {
		t.Fatalf("port = %+v, want mcp.ProviderPort on mcp.Provider, collected", port)
	}
	if len(port.Contributors) != 2 || port.Contributors[0] != "mcp.testMcpProvider" ||
		port.Contributors[1] != "mcp.McpProvider" {
		t.Fatalf("contributors = %v, want [mcp.testMcpProvider mcp.McpProvider] in plugin order", port.Contributors)
	}
}

// Given a path, the tool writes the document and answers with the path alone.
func TestArchitecture_WritesAFileWhenGivenAPath(t *testing.T) {
	broker := testBroker()
	runEngine(t, archPlugin{}, broker)

	path := filepath.Join(t.TempDir(), "nested", "architecture.json")
	arguments, err := json.Marshal(architectureRequest{Path: path})
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
	var onDisk architectureResponse
	if err := json.Unmarshal(written, &onDisk); err != nil {
		t.Fatalf("the written file is not the document: %v", err)
	}
	if len(onDisk.Commands) != 2 {
		t.Fatalf("written document = %+v, want the whole document", onDisk)
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

	for name, request := range map[string]architectureRequest{
		"relative":        {Path: filepath.Join("relative", "architecture.json")},
		"wrong extension": {Path: filepath.Join(t.TempDir(), "architecture.txt")},
	} {
		t.Run(name, func(t *testing.T) {
			arguments, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			result := callTool(t, broker, provider{}.Capabilities()[0], string(arguments))
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

// contendShared is written by both subscriptions on contendSerialEvent, which
// therefore run one at a time; contendLeft and contendRight are written by one
// subscription each on contendParallelEvent, which therefore run together.
type contendShared int
type contendLeft int
type contendRight int

type contendSerialEvent struct{}
type contendParallelEvent struct{}

type contendSerialA kernel.Subscription[contendSerialEvent]
type contendSerialB kernel.Subscription[contendSerialEvent]
type contendParallelA kernel.Subscription[contendParallelEvent]
type contendParallelB kernel.Subscription[contendParallelEvent]

func writesOn[R any, E any]() (kernel.Lock, kernel.Observe[E]) {
	var resource kernel.Write[R]
	return func(access kernel.ResourceAccess) {
			resource = access.GetWrite[R]()
		}, func(kernel.Kernel, E) {
			_ = resource.Get()
		}
}

type contendPlugin struct{}

func (contendPlugin) Name() kernel.PluginName           { return "contend" }
func (contendPlugin) Dependencies() []kernel.PluginName { return nil }

func (contendPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(contendShared(0))
	registrar.InitResource(contendLeft(0))
	registrar.InitResource(contendRight(0))
	registrar.Subscribe[contendSerialA](writesOn[contendShared, contendSerialEvent])
	registrar.Subscribe[contendSerialB](writesOn[contendShared, contendSerialEvent])
	registrar.Subscribe[contendParallelA](writesOn[contendLeft, contendParallelEvent])
	registrar.Subscribe[contendParallelB](writesOn[contendRight, contendParallelEvent])
	return nil
}

// capShared is written by six commands, so fifteen pairs serialise on it: more
// than the ten the report lists.
type capShared int
type capRequest struct{}
type capResponse struct{}

type capCmd1 kernel.Command[capRequest, capResponse]
type capCmd2 kernel.Command[capRequest, capResponse]
type capCmd3 kernel.Command[capRequest, capResponse]
type capCmd4 kernel.Command[capRequest, capResponse]
type capCmd5 kernel.Command[capRequest, capResponse]
type capCmd6 kernel.Command[capRequest, capResponse]

func capWriterImpl() (kernel.Lock, kernel.Execute[capRequest, capResponse]) {
	var shared kernel.Write[capShared]
	return func(access kernel.ResourceAccess) {
			shared = access.GetWrite[capShared]()
		}, func(kernel.Kernel, capRequest) capResponse {
			_ = shared.Get()
			return capResponse{}
		}
}

type capPlugin struct{}

func (capPlugin) Name() kernel.PluginName           { return "cap" }
func (capPlugin) Dependencies() []kernel.PluginName { return nil }

func (capPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(capShared(0))
	registrar.HandleCommand[capCmd1](capWriterImpl)
	registrar.HandleCommand[capCmd2](capWriterImpl)
	registrar.HandleCommand[capCmd3](capWriterImpl)
	registrar.HandleCommand[capCmd4](capWriterImpl)
	registrar.HandleCommand[capCmd5](capWriterImpl)
	registrar.HandleCommand[capCmd6](capWriterImpl)
	return nil
}

// describeRaw is describeEngine with the answer left as its top-level keys, so a
// test can see which keys are present at all.
func describeRaw(t *testing.T, broker *plugin, arguments string) map[string]json.RawMessage {
	t.Helper()
	result := callTool(t, broker, provider{}.Capabilities()[0], arguments)
	if result.IsError {
		t.Fatalf("mcpserver_architecture refused: %s", resultText(t, result))
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(resultText(t, result)), &keys); err != nil {
		t.Fatalf("decode the answer: %v", err)
	}
	return keys
}

func sortedKeys(keys map[string]json.RawMessage) []string {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// A handler that uses a command absorbs its locks, so the two are a pair that
// can never overlap, and the resource they serialise on names both as writers.
func TestArchitecture_ReportsTheUsesPairAsContention(t *testing.T) {
	broker := testBroker()
	runEngine(t, archPlugin{}, broker)

	contention := describeEngine(t, broker, `{}`).Contention
	if contention == nil || !contention.Contended {
		t.Fatalf("contention = %+v, want contended", contention)
	}
	if len(contention.Resources) != 1 {
		t.Fatalf("resources = %+v, want the counter alone", contention.Resources)
	}
	counter := contention.Resources[0]
	if counter.Type != "mcp.archCounter" || counter.Owner != "arch" || counter.Conflicts != 1 ||
		counter.Readers != 0 ||
		!slices.Equal(counter.Writers, []string{"mcp.archInnerCmd", "mcp.archOuterCmd"}) {
		t.Fatalf("counter = %+v, want one pair, both commands writing, no readers", counter)
	}
	if len(contention.HandlerPairs) != 1 || contention.HandlerPairsOmitted != 0 {
		t.Fatalf("pairs = %+v (omitted %d), want the one pair", contention.HandlerPairs,
			contention.HandlerPairsOmitted)
	}
	pair := contention.HandlerPairs[0]
	if pair.A != "mcp.archInnerCmd" || pair.B != "mcp.archOuterCmd" ||
		!slices.Equal(pair.Resources, []string{"mcp.archCounter"}) {
		t.Fatalf("pair = %+v, want inner / outer on the counter", pair)
	}
	if len(contention.Phases) != 0 {
		t.Fatalf("phases = %+v, want none: commands are in no phase", contention.Phases)
	}
}

// A phase whose members all conflict is reported single-threaded, naming the
// members responsible; a phase whose members conflict nowhere is absent from
// the report while its members still appear in subscriptions with their phase.
// That pair of readings is how an Agent tells serialised from parallel.
func TestArchitecture_TellsASerialisedPhaseFromAParallelOne(t *testing.T) {
	broker := testBroker()
	runEngine(t, contendPlugin{}, broker)

	document := describeEngine(t, broker, `{}`)
	if document.Contention == nil || !document.Contention.Contended {
		t.Fatalf("contention = %+v, want contended", document.Contention)
	}
	phases := document.Contention.Phases
	if len(phases) != 1 {
		t.Fatalf("phases = %+v, want the serialised event alone", phases)
	}
	serial := phases[0]
	if serial.Event != "mcp.contendSerialEvent" || serial.Phase != "ordinary" || serial.Members != 2 ||
		serial.Conflicts != 1 || !serial.SingleThreaded ||
		!slices.Equal(serial.WidestLocks, []string{"mcp.contendSerialA", "mcp.contendSerialB"}) {
		t.Fatalf("phase = %+v, want two members, one pair, single-threaded, both widest", serial)
	}

	parallel := 0
	for _, subscription := range document.Subscriptions {
		if subscription.Event == "mcp.contendParallelEvent" && subscription.Phase == "ordinary" {
			parallel++
		}
	}
	if parallel != 2 {
		t.Fatalf("subscriptions = %+v, want both parallel members listed as ordinary", document.Subscriptions)
	}
}

// The pairwise list is quadratic, so it is capped the way kernel.Dump caps it:
// the ten pairs sharing the most, and a count of the rest.
func TestArchitecture_CapsTheHandlerPairsAndCountsTheRest(t *testing.T) {
	broker := testBroker()
	runEngine(t, capPlugin{}, broker)

	contention := describeEngine(t, broker, `{}`).Contention
	if contention == nil || len(contention.HandlerPairs) != 10 || contention.HandlerPairsOmitted != 5 {
		t.Fatalf("contention = %+v, want ten pairs and five omitted", contention)
	}
	if len(contention.Resources) != 1 || contention.Resources[0].Conflicts != 15 ||
		len(contention.Resources[0].Writers) != 6 {
		t.Fatalf("resources = %+v, want the shared one, uncapped at fifteen pairs", contention.Resources)
	}
}

// An engine nothing contends in says so: contended is present and false, rather
// than left for an Agent to infer from missing arrays.
func TestArchitecture_StatesThatNothingContends(t *testing.T) {
	broker := testBroker()
	runEngine(t, broker)

	keys := describeRaw(t, broker, `{}`)
	raw, present := keys["contention"]
	if !present {
		t.Fatalf("keys = %v, want contention present", sortedKeys(keys))
	}
	var contention map[string]json.RawMessage
	if err := json.Unmarshal(raw, &contention); err != nil {
		t.Fatalf("decode contention: %v", err)
	}
	if got := sortedKeys(contention); !slices.Equal(got, []string{"contended"}) {
		t.Fatalf("contention keys = %v, want contended alone", got)
	}
	if string(contention["contended"]) != "false" {
		t.Fatalf("contended = %s, want false", contention["contended"])
	}
}

// Every field a client already parses is still there, and contention is the
// one key added. contendPlugin is composed beside archPlugin only so every
// array has an entry, since an empty one is omitted.
func TestArchitecture_AddsContentionAndChangesNothingElse(t *testing.T) {
	broker := testBroker()
	runEngine(t, archPlugin{}, contendPlugin{}, broker)

	got := sortedKeys(describeRaw(t, broker, `{}`))
	want := []string{"commands", "contention", "plugins", "ports", "resources", "subscriptions"}
	if !slices.Equal(got, want) {
		t.Fatalf("top-level keys = %v, want %v", got, want)
	}
}

// The written file is the same capped document as the inline answer, and the
// path-only reply carries no contention, which would otherwise read as an
// engine without any.
func TestArchitecture_WritesTheContentionToTheFile(t *testing.T) {
	broker := testBroker()
	runEngine(t, capPlugin{}, broker)

	inline := describeRaw(t, broker, `{}`)
	path := filepath.Join(t.TempDir(), "architecture.json")
	arguments, err := json.Marshal(architectureRequest{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(describeRaw(t, broker, string(arguments))); !slices.Equal(got, []string{"path"}) {
		t.Fatalf("path reply keys = %v, want path alone", got)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]json.RawMessage
	if err := json.Unmarshal(written, &onDisk); err != nil {
		t.Fatal(err)
	}
	var fromFile, fromReply architectureContention
	if err := json.Unmarshal(onDisk["contention"], &fromFile); err != nil {
		t.Fatalf("decode the file's contention: %v", err)
	}
	if err := json.Unmarshal(inline["contention"], &fromReply); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromFile, fromReply) || len(fromFile.HandlerPairs) != 10 {
		t.Fatalf("file contention = %+v, want the inline one %+v, capped", fromFile, fromReply)
	}
}

// The description is prompt text, and it has to tell an Agent what contention
// means, how it is cut, and the two readings it would otherwise get wrong.
func TestArchitecture_TheDescriptionExplainsContention(t *testing.T) {
	for _, wanted := range []string{
		"contention", "not a defect", "singleThreaded", "widestLocks", "handlerPairsOmitted",
		"uses a command", "Exclusive", "selfExclusive", "Adapters are bound to each Port",
		"Pass `path`", "you cannot call them from here",
	} {
		if !strings.Contains(architectureDescription, wanted) {
			t.Errorf("the description never says %q", wanted)
		}
	}
	if strings.Contains(architectureDescription, "which plugins contributed") {
		t.Error("the description still says plugins contribute the Adapters")
	}
}
