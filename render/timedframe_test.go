package render

import (
	"encoding/binary"
	"reflect"
	"testing"
	"time"
)

func TestATimedFramesTimingsAreItsPassesTicksAtThePeriodSortedByOrder(t *testing.T) {
	frame := timedFrame{timedPasses: []timedPass{{order: 5, label: "late"}, {order: 1, label: "early"}, {order: 5, label: "late again"}, {order: 1, label: "backwards"}}}
	mapped := binary.LittleEndian.AppendUint64(nil, 10)
	for _, timestamp := range []uint64{30, 100, 104, 7, 9, 50, 40} {
		mapped = binary.LittleEndian.AppendUint64(mapped, timestamp)
	}

	frame.fillTimings(mapped, 2.5)

	want := []PassTiming{
		{Order: 1, Label: "early", Duration: 10 * time.Nanosecond},
		{Order: 1, Label: "backwards"},
		{Order: 5, Label: "late", Duration: 50 * time.Nanosecond},
		{Order: 5, Label: "late again", Duration: 5 * time.Nanosecond},
	}
	if !reflect.DeepEqual(frame.passTimings, want) {
		t.Errorf("the timings are %+v, want %+v", frame.passTimings, want)
	}
}
