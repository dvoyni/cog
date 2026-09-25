package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

var update = flag.Bool("update", false, "rewrite the golden files from what the code produces")

// canvas_draws's JSON is a wire contract an agent reads, so what it emits for
// a frame that sets every optional block, and how it reads a request that
// names, omits or nulls each layer bound, is pinned byte for byte. Requests go
// in as JSON rather than as Go values, so the pin holds whatever the fields
// are spelled as in Go.

// aFrameWithEveryOptional windows a layer, clips a framed sprite and a text,
// and bounds a triangle list on a second layer.
func aFrameWithEveryOptional(queue *OpQueue) {
	queue.SetLayerTransform(1, m.Rect{X: -8, Y: -6, Width: 16, Height: 12}, AspectOverlap)
	queue.Clear(1, m.Color{R: 0.25, A: 1})
	queue.SetClip(m.Rect{X: 1, Y: 2, Width: 30, Height: 40})
	queue.Sprite(1, "images/hero.png", SpriteTransform{
		Position: m.Vec2{X: 10, Y: 20}, Size: m.Vec2{X: 32, Y: 48},
		Frame:  SpriteFrame{Left: 2, Top: 4, Right: 18, Bottom: 20},
		Filter: gfx.FilterNearest,
	}, nil)
	queue.Text(1, "fonts/body.ttf", "score", TextDraw{
		Position: m.Vec2{X: 4, Y: 6}, Size: 12, Color: m.Color{G: 1, A: 1}, Align: AlignCenter,
	})
	queue.RemoveClip()
	queue.DrawTriangles(3, triangleFan(), nil)
}

func TestTheDrawsWireIsPinned(t *testing.T) {
	cases := []struct {
		name, request string
	}{
		{"whole", `{}`},
		{"one-layer", `{"fromLayer":1,"toLayer":1}`},
		{"from-zero", `{"fromLayer":0,"vertices":[2]}`},
		{"nulls", `{"fromLayer":null,"toLayer":null}`},
	}
	rig := newDrawsRig(t)
	rig.fixture.on(aFrameWithEveryOptional)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var request drawsRequest
			if err := json.Unmarshal([]byte(c.request), &request); err != nil {
				t.Fatalf("decode %s: %v", c.request, err)
			}
			reencoded, err := json.Marshal(request)
			if err != nil {
				t.Fatalf("encode request: %v", err)
			}
			response, err := rig.runDraws(request)
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			document, err := json.MarshalIndent(response.DrawsView, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := append(append(reencoded, '\n'), document...)
			golden(t, "draws-"+c.name+".json", append(got, '\n'))
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
