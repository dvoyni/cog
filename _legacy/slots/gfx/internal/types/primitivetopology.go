package types

import "strconv"

// PrimitiveTopology selects how vertices assemble into primitives.
type PrimitiveTopology uint8

const (
	TopologyTriangleList PrimitiveTopology = iota
	TopologyTriangleStrip
	TopologyLineList
)

// String spells the topology the way WebGPU does.
func (topology PrimitiveTopology) String() string {
	switch topology {
	case TopologyTriangleList:
		return "triangle-list"
	case TopologyTriangleStrip:
		return "triangle-strip"
	case TopologyLineList:
		return "line-list"
	}
	return "unknown(" + strconv.Itoa(int(topology)) + ")"
}
