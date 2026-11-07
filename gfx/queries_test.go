package gfx_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

func TestCreateQuerySetAnswersErrQuerySetsUnavailableAndNoQuerySet(t *testing.T) {
	descriptor := gfx.QuerySetDescriptor{Label: "passes", Type: gfx.QueryTypeTimestamp, Count: 8}
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		querySet, err := device.CreateQuerySet(descriptor)
		if !errors.Is(err, gfx.ErrQuerySetsUnavailable) {
			t.Errorf("CreateQuerySet answered %v, want ErrQuerySetsUnavailable", err)
		}
		if querySet != (gfx.QuerySet{}) {
			t.Errorf("CreateQuerySet answered %+v, want the zero query set", querySet)
		}
	})

	calls := h.instance.Recording().Calls
	last := calls[len(calls)-1]
	if last.Kind != recording.CallKindCreateQuerySet || last.Arguments.(recording.CreateQuerySetArguments).Descriptor != descriptor {
		t.Errorf("the last call is %+v, want CreateQuerySet with %+v", last, descriptor)
	}
}

func TestReleaseQuerySetOfNoQuerySetLogsErrReleasedAndReachesNoBackend(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		querySet, _ := device.CreateQuerySet(gfx.QuerySetDescriptor{Type: gfx.QueryTypeTimestamp, Count: 2})
		before := len(h.instance.Recording().Calls)
		device.ReleaseQuerySet(querySet)

		if after := len(h.instance.Recording().Calls); after != before {
			t.Errorf("the release reached the backend: %v", kinds(h.instance.Recording().Calls[before:]))
		}
	})
	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrReleased) {
		t.Fatalf("logged %v, want one ErrReleased", logged)
	}
}

func TestTimestampPeriodIsTheBackendsOne(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		if period := device.TimestampPeriod(); period != recording.TimestampPeriod {
			t.Errorf("TimestampPeriod = %v, want the recording adapter's %v", period, recording.TimestampPeriod)
		}
	})
}

func TestFeatureFlagTimestampQueryIsNeverReported(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		if device.Features()&gfx.FeatureFlagTimestampQuery != 0 {
			t.Errorf("Features = %#x, reporting FeatureFlagTimestampQuery", device.Features())
		}
	})
}

func TestRequiringFeatureFlagTimestampQueryFailsStart(t *testing.T) {
	h := newHarness(t)

	if err := h.spawn(&gfx.Config{RequiredFeatures: gfx.FeatureFlagTimestampQuery}); err == nil {
		t.Fatal("Spawn requiring FeatureFlagTimestampQuery answered no error")
	}
}

func TestPassesWithTimestampWritesOfNoQuerySetAreRecordedAndSubmitted(t *testing.T) {
	timestampWrites := gfx.PassTimestampWrites{BeginningOfPassWriteIndex: 0, EndOfPassWriteIndex: 1}
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		mustSucceed(t, err)
		encoder.BeginComputePass(gfx.ComputePassDescriptor{TimestampWrites: timestampWrites}).End()
		encoder.BeginRenderPass(gfx.RenderPassDescriptor{TimestampWrites: timestampWrites}).End()
		commandBuffer, err := encoder.Finish()
		mustSucceed(t, err)
		mustSucceed(t, device.Submit(commandBuffer))
	})

	want := []recording.Submission{{Buffers: []recording.CommandBuffer{{Commands: []recording.Command{
		{Kind: recording.CommandKindBeginComputePass, Arguments: gfx.ComputePassDescriptor{TimestampWrites: timestampWrites}},
		{Kind: recording.CommandKindBeginRenderPass, Arguments: gfx.RenderPassDescriptor{TimestampWrites: timestampWrites}},
	}}}}}
	if submissions := h.instance.Recording().Submissions; !reflect.DeepEqual(submissions, want) {
		t.Errorf("submissions = %+v, want %+v", submissions, want)
	}
}
