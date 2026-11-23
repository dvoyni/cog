package canvas

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/render"
)

func TestEachFrameWrittenDownTakesOnlyWhatWasNotedSinceTheFrameBeforeAndADeliveryForgetsTheRest(t *testing.T) {
	// One request writes two frames down, as when render joins it to an
	// inspection already pending: each takes its own mesh file. A note
	// after the second, before the delivery, is forgotten with the request,
	// so the next request's frame takes only its own.
	var canvasInspector inspector
	beat := func(tick uint64) metronome.Beat { return metronome.Beat{Tick: tick} }
	canvasInspector.requestPending.Store(true)
	canvasInspector.addLoadedMeshFile(MeshFile{index: 1})
	canvasInspector.record(Inspection{Beat: beat(1)})
	canvasInspector.addLoadedMeshFile(MeshFile{index: 2})
	canvasInspector.record(Inspection{Beat: beat(2)})
	canvasInspector.addLoadedMeshFile(MeshFile{index: 3})
	if !canvasInspector.deliver(beat(2), render.Inspection{}) {
		t.Fatal("the second frame written down was not delivered")
	}
	delivered, _ := canvasInspector.read(beat(2))
	if want := []MeshFile{{index: 2}}; !slices.Equal(delivered.LoadedMeshFiles, want) {
		t.Errorf("the second frame written down loaded %v, want only %v, noted since the first", delivered.LoadedMeshFiles, want)
	}

	canvasInspector.requestPending.Store(true)
	canvasInspector.addLoadedMeshFile(MeshFile{index: 4})
	canvasInspector.record(Inspection{Beat: beat(3)})
	canvasInspector.deliver(beat(3), render.Inspection{})
	next, _ := canvasInspector.read(beat(3))
	if want := []MeshFile{{index: 4}}; !slices.Equal(next.LoadedMeshFiles, want) {
		t.Errorf("the next request's frame loaded %v, want only %v, the note before its request forgotten", next.LoadedMeshFiles, want)
	}
}
