package archtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dvoyni/cog/bundles/anim/animplugin"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene/ecssceneplugin"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/bundles/mcp/mcpplugin"
	"github.com/dvoyni/cog/bundles/scene/sceneplugin"
	"github.com/dvoyni/cog/bundles/ui/uiplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var update = flag.Bool("update", false, "rewrite the golden files from what the code produces")

// The schemas an agent's client receives are a wire contract: a field that
// changes type, drops "null" or turns required breaks every agent already
// written against it, and nothing in a provider's own tests would notice,
// because the broker renders them. So the whole tool list of a full cog
// composition is pinned as the agent reads it, over a client, and a change to
// it is a golden-file diff somebody has to mean.
func TestToolSchemas_WhatAnAgentReadsIsPinned(t *testing.T) {
	addr := freeAddr(t)
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		mcp.Name:     mcp.Config{Addr: addr, Path: "/mcp"},
	}).Handler(func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	}).WithPlugins(
		storageplugin.New(), permanentAdapter{}, appplugin.New(), mainLoopAdapter{}, gfxplugin.New(), backendAdapter{&detachedBackend{}},
		inputplugin.New(), animplugin.New(), canvasplugin.New(), sceneplugin.New(), uiplugin.New(),
		ecsplugin.New(), ecssceneplugin.New(), mcpplugin.New(),
	)
	stopped := make(chan struct{})
	go func() { engine.Run(); close(stopped) }()
	t.Cleanup(func() {
		engine.Quit()
		<-stopped
	})
	<-engine.Ready()

	session := attachClient(t, "http://"+addr+"/mcp")
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	schemas := map[string]any{}
	for _, tool := range tools.Tools {
		schemas[tool.Name] = map[string]any{"input": tool.InputSchema, "output": tool.OutputSchema}
	}
	got, err := json.MarshalIndent(schemas, "", "  ")
	if err != nil {
		t.Fatalf("marshal schemas: %v", err)
	}
	golden(t, "toolschemas.json", append(got, '\n'))
}

// freeAddr is a loopback address nothing is listening on. The broker reports
// the port it bound only through its log line, so the test chooses one first.
func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release the free port: %v", err)
	}
	return addr
}

func attachClient(t *testing.T, endpoint string) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := sdk.NewClient(&sdk.Implementation{Name: "cog-archtest", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: endpoint, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect to %s: %v", endpoint, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// golden compares got with testdata/name, or rewrites the file under -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s is missing; run with -update to capture it", path)
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// A checkout with autocrlf writes the file back with CRLF endings.
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Errorf("%s changed:\n%s", path, got)
	}
}
