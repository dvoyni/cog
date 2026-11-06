package gfx

// Query sets wait on the wgpu fork: v0.34.2 has no public query sets or
// timestamps, so until the fork exposes them every backend answers
// CreateQuerySet with an error and reports no FeatureTimestampQuery. The
// declarations fix the mirror's shape, WebGPU's.

// FeatureTimestampQuery is timestamp queries: query sets of
// QueryTypeTimestamp and pass timestamp writes. A backend reports it only
// where they work.
const FeatureTimestampQuery Features = 0x400

// QueryType is what a query set's queries record.
type QueryType uint32

const (
	// QueryTypeOcclusion counts the samples that passed, between a render
	// pass's occlusion queries.
	QueryTypeOcclusion QueryType = 0x0
	// QueryTypeTimestamp records GPU time at a pass's beginning and end, in
	// ticks of Device.TimestampPeriod nanoseconds.
	QueryTypeTimestamp QueryType = 0x1
)

// QuerySetDescriptor describes a query set.
type QuerySetDescriptor struct {
	Label string
	Type  QueryType
	Count uint32
}

// PassTimestampWrites names where a pass writes its timestamps: the query
// set and the indices for its beginning and end. A zero QuerySet is none.
type PassTimestampWrites struct {
	QuerySet                  QuerySet
	BeginningOfPassWriteIndex uint32
	EndOfPassWriteIndex       uint32
}
