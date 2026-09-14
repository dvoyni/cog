package anim

import "github.com/dvoyni/cog/bundles/anim/internal/types"

// Timeline is one chain of tracks and cues with its own clock; see the package
// documentation for the chain-point model. It is not a resource of its own:
// timelines are handed out by the Timelines resource and are valid only for the
// handler pass that took them. The zero value is an empty timeline at time zero,
// and a nil *Timeline is the no-op timeline.
//
// Add, Query and Value queue and read tracks; Cue, Fired and FiredCues queue
// and read cues; Rewind and Wait move the chain point; Time, Idle and Reset
// read and clear the clock.
type Timeline = types.Timeline

// Params describes how a track plays. The zero value is a zero-duration,
// linear, one-shot track that starts at the chain point. WithEasing, WithLoop
// and WithImmediate return modified copies.
type Params = types.Params

// State is the result of a Query: whether a track matched the slot and, if
// so, whether it is playing now or still pending. State.Found reports whether
// any track, active or pending, matched.
type State = types.State

const (
	// StateNotFound reports that no track is stored under the slot.
	StateNotFound = types.StateNotFound
	// StatePending reports a track that is queued but has not started; its
	// progress is the easing of 0.
	StatePending = types.StatePending
	// StateActive reports a track that is playing now.
	StateActive = types.StateActive
)

// Easing maps normalized progress in [0, 1] to eased progress. A track applies
// its easing to clamped (or, when looping, wrapped) progress before the
// sequence produces a value.
type Easing = types.Easing

// Sequence produces a value of type T for eased progress in [0, 1]. Any type
// may implement it; a track stores the sequence value it was added with, so a
// sequence may carry whatever payload the reader needs at draw time.
type Sequence[T any] = types.Sequence[T]

// Lerp is a Sequence that mixes between two values: At mixes From and To by
// progress with Mix, which must be set; the Lerp* constructors supply the m
// package's mixes. Embed it in a named struct to give a track its own slot type
// and payload fields:
//
//	type MoveSeq struct {
//		anim.Lerp[float32]
//		From, To TileId
//	}
//
//	tl.Add(unitId, MoveSeq{Lerp: anim.LerpFloat(0, 1), From: a, To: b}, anim.Over(0.4))
type Lerp[T any] = types.Lerp[T]

// Flipbook is a Sequence that steps through a fixed list of frames, holding
// each for an equal slice of the track. The frames are the values the track
// produces (sprite declarations, texture paths, whatever the drawing code
// takes), so nothing else has to turn progress into a frame index.
//
// At returns the frame progress falls in, and Params the one-shot linear track
// that plays every frame once at FPS; a flipbook with no frames or a
// non-positive FPS asks for a zero-duration track. Embed it in a named struct
// to give the track its own slot type, as with Lerp:
//
//	type FlagWaveSeq struct{ anim.Flipbook[Sprite] }
//
//	book := anim.Flipbook[Sprite]{Frames: flagFrames, FPS: 30}
//	tl.Add(NoId{}, FlagWaveSeq{book}, book.Params().WithLoop().WithImmediate())
type Flipbook[T any] = types.Flipbook[T]
