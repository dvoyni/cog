package recording_test

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
)

func TestEnabledTimestampsAreReportedAndADeviceRequiringThemIsAnswered(t *testing.T) {
	instance := recording.NewInstance()
	instance.EnableTimestamps()
	adapter, err := instance.RequestAdapter(gfx.RequestAdapterOptions{})
	mustSucceed(t, err)

	if features := adapter.Features(); features&gfx.FeatureFlagTimestampQuery == 0 || features|gfx.FeatureFlagTimestampQuery != ^gfx.FeatureFlags(0) {
		t.Errorf("Features = %#x, want every feature", features)
	}
	if _, err := adapter.RequestDevice(gfx.DeviceDescriptor{RequiredFeatures: gfx.FeatureFlagTimestampQuery}); err != nil {
		t.Errorf("RequestDevice requiring FeatureFlagTimestampQuery answered %v", err)
	}
}

func TestEnablingTimestampsAfterTheAdapterWasRequestedPanics(t *testing.T) {
	instance := recording.NewInstance()
	_, err := instance.RequestAdapter(gfx.RequestAdapterOptions{})
	mustSucceed(t, err)

	defer func() {
		if recover() == nil {
			t.Error("EnableTimestamps after RequestAdapter did not panic")
		}
	}()
	instance.EnableTimestamps()
}

func TestPassTimestampsAreAsManyTicksApartAsThePassesCommandsAndCountOnAcrossSubmits(t *testing.T) {
	_, device := requestTimestampDevice(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	querySet := createQuerySet(t, device, 4)
	resolved := createResolveBuffers(t, device)

	// The compute pass records its beginning and two bind groups, three
	// ticks; the render pass, after it, only its beginning.
	encoder := createEncoder(t, device)
	computePass := encoder.BeginComputePass(gfx.ComputePassDescriptor{TimestampWrites: gfx.PassTimestampWrites{QuerySet: querySet, BeginningOfPassWriteIndex: 0, EndOfPassWriteIndex: 1}})
	computePass.SetBindGroup(0, live.group, nil)
	computePass.SetBindGroup(1, live.group, nil)
	computePass.End()
	encoder.BeginRenderPass(gfx.RenderPassDescriptor{
		ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}},
		TimestampWrites:  gfx.PassTimestampWrites{QuerySet: querySet, BeginningOfPassWriteIndex: 3, EndOfPassWriteIndex: 2},
	}).End()
	resolved.record(encoder, querySet, 4)
	submit(t, device, finish(t, encoder))

	if got, want := resolved.read(t, device, 4), []uint64{0, 3, 4, 3}; !slices.Equal(got, want) {
		t.Errorf("the first submit's timestamps are %v, want %v", got, want)
	}

	// The first buffer recorded six commands, its resolve and copy among
	// them, so the next counts on from six.
	encoder = createEncoder(t, device)
	encoder.BeginComputePass(gfx.ComputePassDescriptor{TimestampWrites: gfx.PassTimestampWrites{QuerySet: querySet, BeginningOfPassWriteIndex: 0, EndOfPassWriteIndex: 1}}).End()
	resolved.record(encoder, querySet, 2)
	submit(t, device, finish(t, encoder))

	if got, want := resolved.read(t, device, 2), []uint64{6, 7}; !slices.Equal(got, want) {
		t.Errorf("the second submit's timestamps are %v, want %v", got, want)
	}
}

func TestTimestampsCountTheCommandsOfAnAdapterRecordingNothing(t *testing.T) {
	instance, device := requestTimestampDevice(t)
	instance.DisableRecording()
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	querySet := createQuerySet(t, device, 2)
	resolved := createResolveBuffers(t, device)

	encoder := createEncoder(t, device)
	computePass := encoder.BeginComputePass(gfx.ComputePassDescriptor{TimestampWrites: gfx.PassTimestampWrites{QuerySet: querySet, BeginningOfPassWriteIndex: 0, EndOfPassWriteIndex: 1}})
	computePass.SetBindGroup(0, live.group, nil)
	computePass.End()
	resolved.record(encoder, querySet, 2)
	submit(t, device, finish(t, encoder))

	if got, want := resolved.read(t, device, 2), []uint64{0, 2}; !slices.Equal(got, want) {
		t.Errorf("the timestamps are %v, want %v", got, want)
	}
}

func TestAKeptQuerySetIsReleasedOnce(t *testing.T) {
	instance, device := requestTimestampDevice(t)
	querySet := createQuerySet(t, device, 2)

	device.ReleaseQuerySet(querySet)
	device.ReleaseQuerySet(querySet)

	calls := instance.Recording().Calls
	first, second := calls[len(calls)-2], calls[len(calls)-1]
	if first.Kind != recording.CallKindReleaseQuerySet || first.Error != nil {
		t.Errorf("the first release was recorded as %+v, want it without an error", first)
	}
	if second.Kind != recording.CallKindReleaseQuerySet || second.Error == nil {
		t.Errorf("the second release was recorded as %+v, want it with an error", second)
	}
}

func TestATimestampQuerySetOnADeviceRequestedWithoutTimestampsIsRefused(t *testing.T) {
	instance := recording.NewInstance()
	instance.EnableTimestamps()
	device := requestDeviceOf(t, instance)

	err := device.CreateQuerySet(handleAt[gfx.QuerySet](1), gfx.QuerySetDescriptor{Type: gfx.QueryTypeTimestamp, Count: 2})
	if err == nil || errors.Is(err, gfx.ErrQuerySetsUnavailable) {
		t.Errorf("CreateQuerySet answered %v, want an error other than ErrQuerySetsUnavailable", err)
	}
}

func TestQuerySetsTimestampsCannotKeepAreRefused(t *testing.T) {
	for _, test := range []struct {
		name       string
		descriptor gfx.QuerySetDescriptor
		want       error
	}{
		{"occlusion queries", gfx.QuerySetDescriptor{Type: gfx.QueryTypeOcclusion, Count: 2}, gfx.ErrQuerySetsUnavailable},
		{"no queries", gfx.QuerySetDescriptor{Type: gfx.QueryTypeTimestamp}, nil},
		{"more queries than WebGPU allows", gfx.QuerySetDescriptor{Type: gfx.QueryTypeTimestamp, Count: 4097}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, device := requestTimestampDevice(t)

			err := device.CreateQuerySet(handleAt[gfx.QuerySet](1), test.descriptor)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Errorf("CreateQuerySet answered %v, want an error", err)
			}
		})
	}
}

func TestTimestampWritesAndResolvesAWebGPUDeviceRefusesFailTheEncoder(t *testing.T) {
	for _, test := range []struct {
		name   string
		record func(encoder gfx.CommandEncoder, querySet gfx.QuerySet, live objects, resolve gfx.Buffer)
	}{
		{"a beginning written past the query set", func(encoder gfx.CommandEncoder, querySet gfx.QuerySet, _ objects, _ gfx.Buffer) {
			encoder.BeginComputePass(gfx.ComputePassDescriptor{TimestampWrites: gfx.PassTimestampWrites{QuerySet: querySet, BeginningOfPassWriteIndex: 2, EndOfPassWriteIndex: 1}}).End()
		}},
		{"an end written past the query set", func(encoder gfx.CommandEncoder, querySet gfx.QuerySet, live objects, _ gfx.Buffer) {
			encoder.BeginRenderPass(gfx.RenderPassDescriptor{
				ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}},
				TimestampWrites:  gfx.PassTimestampWrites{QuerySet: querySet, BeginningOfPassWriteIndex: 0, EndOfPassWriteIndex: 2},
			}).End()
		}},
		{"a beginning and an end at one index", func(encoder gfx.CommandEncoder, querySet gfx.QuerySet, _ objects, _ gfx.Buffer) {
			encoder.BeginComputePass(gfx.ComputePassDescriptor{TimestampWrites: gfx.PassTimestampWrites{QuerySet: querySet, BeginningOfPassWriteIndex: 1, EndOfPassWriteIndex: 1}}).End()
		}},
		{"writes to a query set missing at its index", func(encoder gfx.CommandEncoder, _ gfx.QuerySet, _ objects, _ gfx.Buffer) {
			encoder.BeginComputePass(gfx.ComputePassDescriptor{TimestampWrites: gfx.PassTimestampWrites{QuerySet: handleAt[gfx.QuerySet](2), EndOfPassWriteIndex: 1}}).End()
		}},
		{"a resolve past the query set", func(encoder gfx.CommandEncoder, querySet gfx.QuerySet, _ objects, resolve gfx.Buffer) {
			encoder.ResolveQuerySet(querySet, 1, 2, resolve, 0)
		}},
		{"a resolve of no query set", func(encoder gfx.CommandEncoder, _ gfx.QuerySet, _ objects, resolve gfx.Buffer) {
			encoder.ResolveQuerySet(gfx.QuerySet{}, 0, 2, resolve, 0)
		}},
		{"a resolve into a buffer without QueryResolve", func(encoder gfx.CommandEncoder, querySet gfx.QuerySet, live objects, _ gfx.Buffer) {
			encoder.ResolveQuerySet(querySet, 0, 2, live.buffer, 0)
		}},
		{"a resolve at an offset not a multiple of 256", func(encoder gfx.CommandEncoder, querySet gfx.QuerySet, _ objects, resolve gfx.Buffer) {
			encoder.ResolveQuerySet(querySet, 0, 1, resolve, 8)
		}},
		{"a resolve ending past its buffer", func(encoder gfx.CommandEncoder, querySet gfx.QuerySet, _ objects, resolve gfx.Buffer) {
			encoder.ResolveQuerySet(querySet, 0, 2, resolve, 256)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, device := requestTimestampDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			querySet := createQuerySet(t, device, 2)
			resolve := handleAt[gfx.Buffer](10)
			mustSucceed(t, device.CreateBuffer(resolve, gfx.BufferDescriptor{Size: 264, Usage: gfx.BufferUsageFlagQueryResolve | gfx.BufferUsageFlagCopySrc}))
			encoder := createEncoder(t, device)

			test.record(encoder, querySet, live, resolve)

			if _, err := encoder.Finish(); err == nil {
				t.Error("Finish answered no error")
			}
		})
	}
}

// requestTimestampDevice answers the device of an adapter with timestamps
// enabled, requiring them.
func requestTimestampDevice(t *testing.T) (*recording.Instance, gfx.BackendDevice) {
	t.Helper()
	instance := recording.NewInstance()
	instance.EnableTimestamps()
	adapter, err := instance.RequestAdapter(gfx.RequestAdapterOptions{})
	mustSucceed(t, err)
	device, err := adapter.RequestDevice(gfx.DeviceDescriptor{RequiredFeatures: gfx.FeatureFlagTimestampQuery})
	mustSucceed(t, err)
	return instance, device
}

// createQuerySet creates a timestamp query set of count queries at index 1.
func createQuerySet(t *testing.T, device gfx.BackendDevice, count uint32) gfx.QuerySet {
	t.Helper()
	querySet := handleAt[gfx.QuerySet](1)
	mustSucceed(t, device.CreateQuerySet(querySet, gfx.QuerySetDescriptor{Type: gfx.QueryTypeTimestamp, Count: count}))
	return querySet
}

// resolveBuffers are a buffer queries resolve into and one their results
// are copied into to map.
type resolveBuffers struct {
	resolve, staging gfx.Buffer
}

// createResolveBuffers creates the resolve and staging buffers at indices
// 10 and 11, each of four results.
func createResolveBuffers(t *testing.T, device gfx.BackendDevice) resolveBuffers {
	t.Helper()
	buffers := resolveBuffers{resolve: handleAt[gfx.Buffer](10), staging: handleAt[gfx.Buffer](11)}
	mustSucceed(t, device.CreateBuffer(buffers.resolve, gfx.BufferDescriptor{Size: 32, Usage: gfx.BufferUsageFlagQueryResolve | gfx.BufferUsageFlagCopySrc}))
	mustSucceed(t, device.CreateBuffer(buffers.staging, gfx.BufferDescriptor{Size: 32, Usage: gfx.BufferUsageFlagMapRead | gfx.BufferUsageFlagCopyDst}))
	return buffers
}

// record records the resolve of querySet's first count queries and their
// copy into the staging buffer.
func (b resolveBuffers) record(encoder gfx.CommandEncoder, querySet gfx.QuerySet, count uint32) {
	encoder.ResolveQuerySet(querySet, 0, count, b.resolve, 0)
	encoder.CopyBufferToBuffer(b.resolve, 0, b.staging, 0, 8*uint64(count))
}

// read maps the staging buffer and answers its first count results.
func (b resolveBuffers) read(t *testing.T, device gfx.BackendDevice, count int) []uint64 {
	t.Helper()
	mustSucceed(t, device.MapBuffer(b.staging, gfx.MapModeRead, 0, 32))
	if ready, err := device.MapStatus(b.staging); !ready || err != nil {
		t.Fatalf("MapStatus answered %v, %v, want a resolved map", ready, err)
	}
	mapped, err := device.MappedRange(b.staging, 0, 32)
	mustSucceed(t, err)
	results := make([]uint64, count)
	for index := range results {
		results[index] = binary.LittleEndian.Uint64(mapped[8*index:])
	}
	mustSucceed(t, device.UnmapBuffer(b.staging))
	return results
}

// finish finishes encoder, failing the test on an error.
func finish(t *testing.T, encoder gfx.StandaloneCommandEncoder) gfx.CommandBuffer {
	t.Helper()
	buffer, err := encoder.Finish()
	mustSucceed(t, err)
	return buffer
}
