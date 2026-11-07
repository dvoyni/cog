package recording_test

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

func TestTheAdapterReportsEveryFeatureButTimestampQueries(t *testing.T) {
	adapter, err := recording.NewInstance().RequestAdapter(gfx.RequestAdapterOptions{})
	mustSucceed(t, err)

	if features := adapter.Features(); features != ^gfx.FeatureFlagTimestampQuery {
		t.Errorf("Features = %#x, want every feature but FeatureFlagTimestampQuery", features)
	}
}

func TestADeviceRequestingAFeatureTheAdapterLacksAnswersAnError(t *testing.T) {
	instance := recording.NewInstance()
	adapter, err := instance.RequestAdapter(gfx.RequestAdapterOptions{})
	mustSucceed(t, err)

	device, err := adapter.RequestDevice(gfx.DeviceDescriptor{RequiredFeatures: gfx.FeatureFlagDepthClipControl | gfx.FeatureFlagTimestampQuery})
	if err == nil || device != nil {
		t.Fatalf("RequestDevice answered %v, %v, want an error and no device", device, err)
	}
	if calls := instance.Recording().Calls; calls[len(calls)-1].Kind != recording.CallKindRequestDevice || calls[len(calls)-1].Error == nil {
		t.Errorf("the request was recorded as %+v, want RequestDevice with its error", calls[len(calls)-1])
	}
}

func TestCreateQuerySetIsRecordedAndAnswersErrQuerySetsUnavailable(t *testing.T) {
	instance, device := requestDevice(t)
	querySet := handleAt[gfx.QuerySet](1)
	descriptor := gfx.QuerySetDescriptor{Type: gfx.QueryTypeTimestamp, Count: 2}

	if err := device.CreateQuerySet(querySet, descriptor); !errors.Is(err, gfx.ErrQuerySetsUnavailable) {
		t.Errorf("CreateQuerySet answered %v, want ErrQuerySetsUnavailable", err)
	}
	calls := instance.Recording().Calls
	want := recording.CreateQuerySetArguments{QuerySet: querySet, Descriptor: descriptor}
	if last := calls[len(calls)-1]; last.Kind != recording.CallKindCreateQuerySet || last.Arguments != want || !errors.Is(last.Error, gfx.ErrQuerySetsUnavailable) {
		t.Errorf("the last call is %+v, want CreateQuerySet of %+v with ErrQuerySetsUnavailable", last, want)
	}
}

func TestReleaseQuerySetIsRecordedWithAnErrorSinceNoQuerySetExists(t *testing.T) {
	instance, device := requestDevice(t)
	querySet := handleAt[gfx.QuerySet](1)

	device.ReleaseQuerySet(querySet)

	calls := instance.Recording().Calls
	if last := calls[len(calls)-1]; last.Kind != recording.CallKindReleaseQuerySet || last.Arguments != querySet || last.Error == nil {
		t.Errorf("the last call is %+v, want ReleaseQuerySet of %+v with an error", last, querySet)
	}
}

func TestTimestampPeriodIsOneNanosecond(t *testing.T) {
	_, device := requestDevice(t)

	if period := device.TimestampPeriod(); period != recording.TimestampPeriod || recording.TimestampPeriod != 1 {
		t.Errorf("TimestampPeriod = %v, want 1", period)
	}
}

func TestPassTimestampWritesOfNoQuerySetFinish(t *testing.T) {
	_, device := requestDevice(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	encoder := createEncoder(t, device)
	timestampWrites := gfx.PassTimestampWrites{BeginningOfPassWriteIndex: 2, EndOfPassWriteIndex: 3}

	encoder.BeginRenderPass(gfx.RenderPassDescriptor{
		ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}},
		TimestampWrites:  timestampWrites,
	}).End()
	encoder.BeginComputePass(gfx.ComputePassDescriptor{TimestampWrites: timestampWrites}).End()

	if _, err := encoder.Finish(); err != nil {
		t.Errorf("Finish answered %v, want none", err)
	}
}

func TestACommandNamingAQuerySetFailsAtFinishSinceNoneExists(t *testing.T) {
	for _, test := range []struct {
		name   string
		record func(encoder gfx.CommandEncoder, live objects)
	}{
		{"a render pass writing timestamps", func(encoder gfx.CommandEncoder, live objects) {
			encoder.BeginRenderPass(gfx.RenderPassDescriptor{
				ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}},
				TimestampWrites:  gfx.PassTimestampWrites{QuerySet: handleAt[gfx.QuerySet](1), EndOfPassWriteIndex: 1},
			}).End()
		}},
		{"a compute pass writing timestamps", func(encoder gfx.CommandEncoder, _ objects) {
			encoder.BeginComputePass(gfx.ComputePassDescriptor{
				TimestampWrites: gfx.PassTimestampWrites{QuerySet: handleAt[gfx.QuerySet](1), EndOfPassWriteIndex: 1},
			}).End()
		}},
		{"a resolve of a query set", func(encoder gfx.CommandEncoder, live objects) {
			encoder.ResolveQuerySet(handleAt[gfx.QuerySet](1), 0, 2, live.buffer, 0)
		}},
		{"a resolve of no query set", func(encoder gfx.CommandEncoder, live objects) {
			encoder.ResolveQuerySet(gfx.QuerySet{}, 0, 2, live.buffer, 0)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			encoder := createEncoder(t, device)

			test.record(encoder, live)

			if _, err := encoder.Finish(); err == nil {
				t.Error("Finish answered no error")
			}
		})
	}
}
