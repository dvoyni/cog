package types

import "strconv"

// DrawParamsSet is where one set of draw params is in its life. The zero state
// is an id NewDrawParams never handed out, which is what a gap in any table
// reads as.
type DrawParamsSet uint32

const (
	DrawParamsSetUnknown DrawParamsSet = iota
	DrawParamsSetLive
	// DrawParamsSetFailed is a set whose creation was refused - its shader had
	// no program. It exists, so naming it is no mistake, and it draws nothing.
	DrawParamsSetFailed
	DrawParamsSetReleased
)

func (s DrawParamsSet) String() string {
	switch s {
	case DrawParamsSetUnknown:
		return "unknown"
	case DrawParamsSetLive:
		return "live"
	case DrawParamsSetFailed:
		return "failed"
	case DrawParamsSetReleased:
		return "released"
	}
	return "unknown(" + strconv.Itoa(int(s)) + ")"
}
