package mux

import (
	"reflect"
	"testing"
)

// TestMakeMapAccessPicksTheAccessATypedLookupTakes holds makeMapAccess to the
// access the compiler calls for a typed lookup of each map, as
// go build -gcflags=-S shows it on a 64-bit target.
func TestMakeMapAccessPicksTheAccessATypedLookupTakes(t *testing.T) {
	for _, test := range []struct {
		mapType reflect.Type
		want    mapAccess
	}{
		{reflect.TypeFor[map[uint32]int](), mapAccessFast32},
		{reflect.TypeFor[map[int32]int](), mapAccessFast32},
		{reflect.TypeFor[map[cell]int](), mapAccessFast32},
		{reflect.TypeFor[map[[2]uint16]int](), mapAccessFast32},
		{reflect.TypeFor[map[[4]bool]int](), mapAccessFast32},
		// A zero-size field before another is not padded.
		{reflect.TypeFor[map[struct {
			Marks [0]uint32
			Rank  uint32
		}]int](), mapAccessFast32},
		{reflect.TypeFor[map[uint64]int](), mapAccessFast64},
		{reflect.TypeFor[map[int]int](), mapAccessFast64},
		{reflect.TypeFor[map[sector]int](), mapAccessFast64},
		{reflect.TypeFor[map[struct{ Unit *unit }]int](), mapAccessFast64},
		{reflect.TypeFor[map[chan int]int](), mapAccessFast64},
		{reflect.TypeFor[map[unitName]int](), mapAccessFastString},
		{reflect.TypeFor[map[callsign]int](), mapAccessFastString},
		{reflect.TypeFor[map[[1]string]int](), mapAccessFastString},
		{reflect.TypeFor[map[struct{ Callsigns [1]callsign }]int](), mapAccessFastString},
		{reflect.TypeFor[map[[2]string]int](), mapAccessGeneric},
		{reflect.TypeFor[map[struct{ _ string }]int](), mapAccessGeneric},
		// An element of 128 bytes is kept in the map's slots; one larger is
		// not.
		{reflect.TypeFor[map[uint32][32]float32](), mapAccessFast32},
		{reflect.TypeFor[map[uint32]annal](), mapAccessGeneric},
		{reflect.TypeFor[map[unitName]annal](), mapAccessGeneric},
		{reflect.TypeFor[map[bool]int](), mapAccessGeneric},
		{reflect.TypeFor[map[int8]int](), mapAccessGeneric},
		{reflect.TypeFor[map[int16]int](), mapAccessGeneric},
		{reflect.TypeFor[map[float32]int](), mapAccessGeneric},
		{reflect.TypeFor[map[struct{ Weight float32 }]int](), mapAccessGeneric},
		{reflect.TypeFor[map[[1]float64]int](), mapAccessGeneric},
		{reflect.TypeFor[map[post]int](), mapAccessGeneric},
		{reflect.TypeFor[map[struct{ _ uint32 }]int](), mapAccessGeneric},
		{reflect.TypeFor[map[struct {
			Rank uint16
			_    uint16
		}]int](), mapAccessGeneric},
		// A zero-size field ending a struct is padded.
		{reflect.TypeFor[map[struct {
			Rank  uint32
			Marks [0]uint32
		}]int](), mapAccessGeneric},
		{reflect.TypeFor[map[chronicle]int](), mapAccessGeneric},
		{reflect.TypeFor[map[struct {
			Name string
			Rank int
		}]int](), mapAccessGeneric},
	} {
		if got := makeMapAccess(test.mapType); got != test.want {
			t.Errorf("a %v is read through access %d, but a typed lookup takes %d", test.mapType, got, test.want)
		}
	}
}
