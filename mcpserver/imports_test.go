package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// brokerOnlyModules are the two dependencies that exist for the broker alone.
// Schema inference lives in a separate module from the SDK, so there are two
// rather than one.
var brokerOnlyModules = []string{
	"github.com/modelcontextprotocol/go-sdk",
	"github.com/google/jsonschema-go",
}

// Nothing outside the broker may acquire the MCP SDK or the schema library.
// That absence is the whole reason mcp and mcpserver are two packages: an app
// importing gfx should not end up with an HTTP server and a JSON-schema
// library in its module graph.
func TestImports_BrokerDependenciesReachNoOtherPackage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go tool is needed to resolve the import graph")
	}
	others := slices.DeleteFunc(goList(t, "./..."), func(pkg string) bool {
		return pkg == "github.com/dvoyni/cog/mcpserver"
	})
	if len(others) < 2 {
		t.Fatalf("go list found %d packages besides the broker", len(others))
	}

	if !clean(goList(t, append([]string{"-deps"}, others...)...)) {
		// Only now pay for one invocation per package, to name the offender.
		for _, pkg := range others {
			if !clean(goList(t, "-deps", pkg)) {
				t.Errorf("%s reaches a broker-only module through its imports", pkg)
			}
		}
		t.FailNow()
	}
}

func clean(deps []string) bool {
	for _, dep := range deps {
		for _, module := range brokerOnlyModules {
			if dep == module || strings.HasPrefix(dep, module+"/") {
				return false
			}
		}
	}
	return true
}

func goList(t *testing.T, args ...string) []string {
	t.Helper()
	command := exec.Command("go", append([]string{"list"}, args...)...)
	command.Dir = ".."
	out, err := command.Output()
	if err != nil {
		t.Fatalf("go list %v: %v", args, err)
	}
	return strings.Fields(string(out))
}

// No build tag excludes the broker from any platform. Dropping it on js would
// break a main.go shared between desktop and web builds at compile time, which
// is a worse failure than the one build that is actually wrong failing loudly
// at startup with the address in the error.
func TestImports_NoBuildTagExcludesThePlugin(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no sources found")
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "//go:build") {
			t.Errorf("%s carries a build constraint", source)
		}
	}
}
