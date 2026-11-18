package canvas

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func TestACamerasViewportRoundsEachEdgeToAPixelAndClampsItToTheTarget(t *testing.T) {
	for _, viewport := range []struct {
		name     string
		fraction m.Rect
		want     m.Recti
	}{
		{"zero, the whole target", m.Rect{}, m.Recti{Width: 100, Height: 50}},
		{"the left third", m.Rect{Width: 1.0 / 3, Height: 1}, m.Recti{Width: 33, Height: 50}},
		{"the middle third, sharing the left third's edge", m.Rect{X: 1.0 / 3, Width: 1.0 / 3, Height: 1}, m.Recti{X: 33, Width: 34, Height: 50}},
		{"reaching past the target's bottom right", m.Rect{X: 0.5, Y: 0.5, Width: 1, Height: 1}, m.Recti{X: 50, Y: 25, Width: 50, Height: 25}},
		{"wholly left of the target", m.Rect{X: -1, Width: 0.5, Height: 1}, m.Recti{Height: 50}},
		{"of a negative width", m.Rect{X: 0.5, Width: -0.25, Height: 1}, m.Recti{X: 50, Height: 50}},
	} {
		camera := Camera{Viewport: viewport.fraction}
		if got := camera.placeViewport(100, 50); got != viewport.want {
			t.Errorf("a viewport %s, %+v, of a target 100×50 is %+v, want %+v", viewport.name, viewport.fraction, got, viewport.want)
		}
	}
}

func TestACamerasLayerRunsAreItsRangesEntriesMergedWhereTheyOverlapOrMeet(t *testing.T) {
	// Two entries in each of layers -1 to 3.
	var shapes []Shape
	for layer := int32(3); layer >= -1; layer-- {
		shapes = append(shapes, Shape{Layer: layer}, Shape{Layer: layer, Order: 1})
	}
	sorter := makeEntrySorter(nil)
	entries := sorter.sort(shapes)
	for _, layers := range []struct {
		name   string
		ranges [4]LayerRange
		runs   [][]int32
	}{
		{"none", [4]LayerRange{}, [][]int32{{-1, -1, 0, 0, 1, 1, 2, 2, 3, 3}}},
		{"an empty range", MakeLayers(LayerRange{From: 2, To: 2}), [][]int32{{-1, -1, 0, 0, 1, 1, 2, 2, 3, 3}}},
		{"0 to 2", MakeLayers(LayerRange{From: 0, To: 2}), [][]int32{{0, 0, 1, 1}}},
		{"layer 1 alone", MakeLayer(1), [][]int32{{1, 1}}},
		{"past every entry", MakeLayers(LayerRange{From: 5, To: 9}), nil},
		{"two apart, out of order, and an empty one", [4]LayerRange{{From: 3, To: 4}, {From: 1, To: 1}, {From: -1, To: 0}}, [][]int32{{-1, -1}, {3, 3}}},
		{"overlapping and meeting", [4]LayerRange{{From: 2, To: 4}, {From: 0, To: 1}, {From: 1, To: 3}}, [][]int32{{0, 0, 1, 1, 2, 2, 3, 3}}},
		{"one inside another", [4]LayerRange{{From: -1, To: 3}, {From: 0, To: 1}}, [][]int32{{-1, -1, 0, 0, 1, 1, 2, 2}}},
	} {
		camera := Camera{Layers: layers.ranges}
		var runs [][]int32
		for _, run := range camera.findLayerRuns(entries) {
			if len(run) == 0 {
				continue
			}
			var runLayers []int32
			for _, entry := range run {
				runLayers = append(runLayers, shapes[entry.shapeIndex].Layer)
			}
			runs = append(runs, runLayers)
		}
		if !reflect.DeepEqual(runs, layers.runs) {
			t.Errorf("a camera of %s has runs of the layers %v, want %v", layers.name, runs, layers.runs)
		}
	}
}

func TestCamerasDrawByOrderThenInTheOrderSet(t *testing.T) {
	cameras := []Camera{
		{Order: 1, ClearColor: m.Color{R: 1}},
		{Order: 0, ClearColor: m.Color{R: 2}},
		{Order: 1, ClearColor: m.Color{R: 3}},
		{Order: 0, ClearColor: m.Color{R: 4}},
		{Order: -1, ClearColor: m.Color{R: 5}},
	}

	slices.SortStableFunc(cameras, compareCameras)

	var got []float32
	for _, camera := range cameras {
		got = append(got, camera.ClearColor.R)
	}
	if want := []float32{5, 2, 4, 1, 3}; !slices.Equal(got, want) {
		t.Errorf("the cameras, named by their colours' red, draw in the order %v, want %v", got, want)
	}
}

func TestMakeLayersFillsUpToFourRangesAndPanicsPastThem(t *testing.T) {
	first := LayerRange{From: 0, To: 2}
	second := LayerRange{From: 5, To: 6}
	if got, want := MakeLayers(first, second), [4]LayerRange{first, second}; got != want {
		t.Errorf("two ranges made layers %v, want %v", got, want)
	}
	if got := MakeLayers(); got != ([4]LayerRange{}) {
		t.Errorf("no range made layers %v, want four empty ones", got)
	}
	defer func() {
		if recover() == nil {
			t.Errorf("five ranges made layers, want a panic")
		}
	}()
	MakeLayers(first, first, first, first, second)
}

func TestMakeLayerPanicsForTheLayerNoHalfOpenRangeHolds(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("layer math.MaxInt32 made layers, want a panic")
		}
	}()
	MakeLayer(math.MaxInt32)
}
