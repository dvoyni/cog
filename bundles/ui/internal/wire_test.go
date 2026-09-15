package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/dvoyni/cog/bundles/ui"
)

var update = flag.Bool("update", false, "rewrite the golden files from what the code produces")

// ui_layout's JSON is a wire contract an agent reads, so what it emits for a
// tree that declares every optional value, and how it reads a request that
// names, omits or nulls each filter, is pinned byte for byte. Requests go in
// as JSON rather than as Go values, so the pin holds whatever the fields are
// spelled as in Go.

// aDeclaringTree is a grid whose children declare every length, weight, layer
// and count a DeclaredView can carry, pixel and relative both.
func aDeclaringTree(frame *ui.Frame) {
	frame.Add(10, ui.NewElement().
		ID("grid").
		Width(300).Height(200).
		MinWidth(10).MaxHeightRel(0.9).
		Padding(4, 6).
		Layout(ui.LayoutGrid).Columns(2).Rows(2).GapRel(0.1).
		Children(
			ui.NewElement().
				ID("pinned").
				Left(5).RightRel(0.1).Top(3).BottomRel(0.2).
				PivotLeft(1).PivotTopRel(0.5).
				Stretch(2).Shrink(0).
				Layer(2).Align(ui.AlignEnd).
				Visual(&snapshotTestVisual{}, nil),
			ui.NewElement().
				ID("flex").
				Layout(ui.LayoutHorizontal).Wrap().Gap(2).
				ChildrenArrangement(ui.ArrangeSpaceBetween).
				Children(
					ui.NewElement().Width(0).Shrink(1),
					ui.NewElement().HeightRel(0.5),
				),
			ui.NewElement().IgnoreLayout().Height(0),
		))
}

func TestTheLayoutWireIsPinned(t *testing.T) {
	cases := []struct {
		name, request string
	}{
		{"whole", `{}`},
		{"subtree", `{"subtree":2}`},
		{"depth-zero", `{"subtree":0,"maxDepth":0}`},
		{"nulls", `{"subtree":null,"maxDepth":null}`},
	}
	rig := newLayoutRig(t)
	rig.fixture.on(aDeclaringTree)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var request layoutRequest
			if err := json.Unmarshal([]byte(c.request), &request); err != nil {
				t.Fatalf("decode %s: %v", c.request, err)
			}
			reencoded, err := json.Marshal(request)
			if err != nil {
				t.Fatalf("encode request: %v", err)
			}
			response, err := rig.runLayout(request)
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			document, err := json.MarshalIndent(response.LayoutView, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := append(append(reencoded, '\n'), document...)
			golden(t, "layout-"+c.name+".json", append(got, '\n'))
		})
	}
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
