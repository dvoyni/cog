package internal

import (
	"time"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
)

// VoiceSlot identifies a voice to an Adapter. It is always in [0, MaxVoices),
// minted and recycled by sound, which is what lets an Adapter's voice table be
// a fixed array indexed directly by it.
//
// Voice's index/generation split is deliberately not public contract, so it
// cannot cross into an Adapter without exporting the accessors the handle
// refuses - so it does not cross at all. sound guarantees a slot is stopped
// before it is reused, so the Adapter never sees an ambiguous id and needs no
// generation of its own.
type VoiceSlot int

// ClipID is an opaque handle minted by the Adapter. Zero means none.
type ClipID uint32

// LoopRegion is the span a looping Voice repeats between, in seconds. It is a
// fact about a Clip rather than a parameter of a Voice: it rides in the Ogg
// file's Vorbis comment header and the Adapter parses it in the same pass that
// already reads duration, channels and rate.
type LoopRegion struct{ Start, End float32 }

// VoiceParams is everything about how a voice sounds, as the Adapter sees it.
// Sixteen bytes, and no equation below the seam.
type VoiceParams struct {
	// Gains maps source channels to output channels: Gains[src][out]. Row 0 is
	// a mono clip's only row. sound computes it from the voice's volume, its
	// bus, its falloff and cone and the W3C panning equations; the Adapter
	// multiplies and nothing else.
	//
	// A scalar pan cannot express W3C's stereo rule, and mixing a stereo Clip
	// down to dodge that was refused, so all four entries are ordinary traffic.
	Gains [2][2]float32
	// Rate is the playback rate, 1 being the clip's own.
	Rate float32
	// Paused stops the voice advancing without ending it. It is a field rather
	// than a verb because zeroing a gain does not suspend: a resident buffer
	// started with start() keeps advancing whatever its gain is.
	Paused bool
}

// VoiceStart begins one voice in one slot. A Seek is a VoiceStart with an
// Offset, which is also how a web Adapter's sample-accurate start(when, offset)
// is reached without the seam naming a sample.
type VoiceStart struct {
	Slot   VoiceSlot
	Clip   ClipID
	Offset time.Duration // where to begin; 0 is the head
	Loop   bool
	Params VoiceParams
}

// VoiceUpdate restates one slot's parameters. sound emits target values once
// per flush and the Adapter ramps to them across its own blocks.
type VoiceUpdate struct {
	Slot   VoiceSlot
	Params VoiceParams
}

// Batch is one tick's operations, handed over in a single Emit. It is owned by
// sound and valid only for the duration of that call, which is gfx.Execute's
// shape one Slot over.
//
// An Adapter applies the stops first, then the starts, then the updates. The
// fields are not commutative on one slot and a steal states both of them about
// it in one tick: the victim's stop, and the start that takes its slot over.
// That is the whole of what "sound stops a slot before it reuses one" asks of
// an Adapter, and it is why stealing needs no verb of its own down here.
//
// Destroys is the only way sound releases a Clip, and there is deliberately no
// Backend.Destroy beside it. A destroy inside the batch is guaranteed to arrive
// after the stops that precede it, which is what lets an Adapter free a Clip
// once the Mixer has passed that batch and never while it is still mixing one;
// a bare call outside the batch carries no such ordering, and two ways to say
// one release are what a later reader tries to unify and gets wrong.
type Batch struct {
	Starts   []VoiceStart
	Updates  []VoiceUpdate
	Stops    []VoiceSlot // applied first, before the starts that reuse their slots
	Destroys []ClipID    // ordered after the stops that precede them
}

// reset empties b for the next tick without giving its capacity back, so a
// flush allocates nothing once the engine is warm. It is unexported because an
// Adapter is lent a Batch for the length of one Emit and clearing it there
// would be clearing the tick out from under the call.
func (b *Batch) reset() {
	b.Starts = b.Starts[:0]
	b.Updates = b.Updates[:0]
	b.Stops = b.Stops[:0]
	b.Destroys = b.Destroys[:0]
}

// PreparedClip is a Clip turned into something a Voice can be started from.
// Which kind it is - shared samples, or bytes and a way to read them - is the
// Adapter's own business and a game cannot tell.
//
// It stays opaque about samples and honest about facts. It carries these four
// because VoiceEndedEvent fires for every ending: if it were a bare any, only
// the Adapter would know when a Clip runs out, nosound would never end a Voice,
// and a game tested under nosound would behave differently from the same game
// under otosound.
type PreparedClip interface {
	Duration() float32
	Channels() int
	SampleRate() int
	LoopRegion() m.Maybe[LoopRegion] // absent: loop the whole clip
}

// Prepared is one completed prepare, drained at the top of a flush. Token is
// the value sound handed to Prepare, and is how a completion finds the entry it
// belongs to - or fails to, when that entry was released while the prepare was
// in flight.
type Prepared struct {
	Token any
	Clip  PreparedClip
	Err   error
}

// Device reports the sound device as it is now. It is read-only: a game cannot
// choose a backend, only see which one it got.
type Device struct {
	// Ready reports whether anything is audible. False means absent, not opened
	// yet, or lost; a game cannot tell which, and does not need to.
	Ready bool
	// Name is the Adapter's own name: "otosound", "jssound", "nosound".
	Name string
	// SampleRate and Channels are the device's, not a clip's. Zero until Ready.
	SampleRate int
	Channels   int
	// Latency is the real output latency, not a figure from a Config. Zero
	// until Ready. The contract promises ordering and causality and never a
	// latency figure, so this is the only place one is honest.
	Latency time.Duration
}

// Backend is the interface sound's Adapter implements: a dumb, allocation-free
// sink with a fixed table of slots. It is told how many slots exist once,
// receives one batch per tick, multiplies a gain matrix it did not compute, and
// is asked two questions. It knows nothing of handles, Buses, priorities,
// positions, the cap or the equations.
//
// Nothing is pushed. Every return path is a poll at flush, because a callback
// would arrive on the device thread or in a JS callback, where there is no
// handler and no lock. And there is nothing to poll about Voices at all: sound
// computes endings from Duration and the Voice's rate, so an Adapter never
// reports a Voice.
type Backend interface {
	// Voices tells the Adapter how many slots exist. Called once at startup,
	// before any Emit. Slots are numbered [0, n) and sound stops a slot before
	// it reuses one.
	Voices(n int)

	// Emit applies one tick's operations. batch is owned by sound and is valid
	// only for the duration of the call.
	Emit(batch *Batch)

	// Device reports the device as it is now, including Ready. Polled once per
	// flush; it is a field read, not a query.
	Device() Device

	// Prepare turns encoded Ogg bytes into something voices can be started
	// from: samples for a short clip, the bytes and a way to stream them for a
	// long one. Which, is the Adapter's business. done=true means prepared is
	// valid now; done=false means it will appear in TakePrepared under the same
	// token. The prepared value must be garbage-collectable.
	Prepare(token any, encoded assets.Blob) (prepared PreparedClip, done bool, err error)

	// TakePrepared returns the prepares that finished since the last call, and
	// clears them.
	TakePrepared() []Prepared

	// Install mints the id for a prepared clip. It is Install rather than
	// Prepare that mints, so the handle comes into existence on sound's tick,
	// where the destroy that pairs with it is guaranteed to be statable.
	Install(PreparedClip) (ClipID, error)
}
