//go:build !js

package otosound

// clipRelease is a clip sfx destroyed that the flush side still holds, since
// the mixer may be mid-copy out of its samples, until the mixer has applied
// the changes that went out as sequence, which carried its destroy. The
// destroy travels inside those changes, after the stops that drop the voice
// table's references, so the mixer never frees and never reads freed memory,
// and the flush never waits. Freeing is the garbage collector's, once the
// last reference on the flush side goes.
type clipRelease struct {
	clip     *preparedClip
	sequence uint64
}
