package sound

import "github.com/dvoyni/cog/slots/sound/internal/types"

// Queue is where every operation is recorded, under a write lock, and it is
// flushed once per tick, atomically. It is deliberately not gfx's latest-wins
// triple buffer: a frame is a complete description and may be replaced, an
// operation is a delta and may not.
//
// Access it only while a handler holds its declared resource lock; do not
// retain it after the handler returns.
type Queue = types.Queue

// Clips is sound's own clip table, sitting in front of the Library that holds
// the encoded bytes. It is read-only to a caller: loading and releasing live
// inside sound, and there is no handle to a Clip for a game to hold.
//
// It carries no reader yet. What gameplay legitimately wants of a Clip is a
// question rather than a handle - how long is it, how many channels, and is it
// resident yet - and that question arrives with Preload and Release in issue
// 484. The resource is here now because the flush writes it, and a resource is
// where state that outlives a handler belongs.
//
// Access it only while a handler holds its declared resource lock.
type Clips = types.Clips

// Voices is the live Voice view: handle, Clip, Bus, playhead, duration, Params
// and paused, for every Voice that exists now. It exposes no mutators at all,
// which is what makes a read lock on it safe beside the Systems recording into
// the Queue.
//
// A reader can rely on this: every Voice that ever existed is observable
// exactly once - it is in this view now, or its VoiceEndedEvent has fired.
//
// Access it only while a handler holds its declared resource lock.
type Voices = types.Voices

// Buses is every Bus's volume as of the last flush, read the way the Voices
// are. A settings screen reads it to draw its slider where the player left it,
// and records SetBus on the Queue to move it; there is no mutator here, and no
// Mute, because a game persists its settings anyway and a shadow copy inside
// sound invites "muted by whom".
//
// A Bus's volume is folded into every Voice on it before the batch leaves, so
// nothing about a Bus reaches an Adapter at all.
//
// Access it only while a handler holds its declared resource lock.
type Buses = types.Buses

// Listener is where the game is heard from as of the last flush, read the way
// the Buses are. There is one per Engine, at the origin and unrotated before
// any call, and it is moved by recording SetListener on the Queue.
//
// It is a resource of its own for the reason sound's resources are several
// rather than one: a System asking where the Listener is never contends with
// the Systems recording operations. sound never reads a camera - a game, or
// ecsaudio, copies a camera's Transform across.
//
// Access it only while a handler holds its declared resource lock.
type Listener = types.Listener

// Device reports the sound device as it is now: whether anything is audible,
// which Adapter got it, and the rate, channels and real latency it runs at. It
// is read-only, because a game neither opens nor chooses a Device; it only
// reads whether there is one.
//
// Audio never stops a game from starting, so a Device that is missing, late or
// lost is a fact a game reads rather than an error it must handle. On web, what
// a game does with it is draw the "click to enable sound" prompt.
//
// Access it only while a handler holds its declared resource lock.
type Device = types.Device
