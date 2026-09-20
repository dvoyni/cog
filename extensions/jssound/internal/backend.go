//go:build js

package internal

import (
	"errors"
	"sync"
	"syscall/js"

	"github.com/dvoyni/cog/extensions/jssound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/sound"
)

// errNotOurClip is Install handed a prepared value this Adapter did not make. It
// is unexported and untyped because nothing can reach it: sound installs only
// what this Adapter's own Prepare produced.
var errNotOurClip = errors.New("jssound: install was handed a prepared Clip this Adapter did not make")

// minTimeConstant floors the ramp shape for a context that reports no usable
// sample rate. A time constant of zero is a step, which is the click.
const minTimeConstant = float64(renderQuantum) / 48000

// slot is one of sound's voice slots as this Adapter holds it: the chain now
// playing, and at most one chain ramping away behind it.
//
// The retired one is kept because a steal states a stop and a start on one slot
// in one batch, and the two are not commutative: the Voice being stolen has to
// ramp to silence on its own gain nodes while the Voice that took its slot plays
// at full gain through its own. One retired chain per slot is all a steal ever
// needs, because sound stops a slot before it reuses one.
type slot struct {
	live      *chain
	retired   *chain
	retiredAt float64
}

// backend is jssound's Adapter. Every method of it is called from sound's flush,
// which holds a write lock on all of sound's resources, so the tables below are
// touched from one place at a time.
//
// There is no device thread here and nothing crossing to one. The browser owns
// the audio thread, on the far side of a node graph that Go cannot reach, which
// is sound.md's second obligation holding vacuously - and the whole reason this
// Adapter exists rather than otosound being built for wasm.
//
// What is concurrent is only Go's own scheduler: a decodeAudioData callback and
// the goroutine the wasm decoder runs on both land outside the flush. They meet
// the flush at one mutex, over one queue, and nowhere else.
type backend struct {
	// audio is the Engine's AudioContext, and is nil on a page that has no Web
	// Audio at all. A nil one is the whole of "the Adapter then behaves exactly
	// as nosound does": every operation is accepted, every Clip still prepares
	// from its headers and installs, and nothing is audible.
	audio *webAudio
	// gestureTarget is what the resume listener was installed on, kept so it can
	// be taken off the same object at Stop.
	gestureTarget js.Value
	// timeConstant is the browser's render quantum in seconds, which every ramp
	// in this Adapter is shaped over.
	timeConstant float64
	// limit is Config.DecodedClipLimit as it was given, sentinels and all. It
	// draws the line between the two tiers, and nothing above the seam can see
	// which side a Clip fell on.
	limit int

	// device is what Device reports. It is a plain field written when the
	// context's state changes, so Device() stays the field read sound polls once
	// per flush rather than a property read across the JS boundary.
	device sound.Device

	slots  []slot
	clips  map[sound.ClipID]*clipData
	lastID sound.ClipID

	// support is the answer to the Ogg probe, and waiting is the prepares that
	// arrived before it settled. A Clip sent down the wrong route would be the
	// probe doing nothing, so they wait rather than guess - which costs the
	// first Clip of a session one decode of an 8 KB micro-clip.
	//
	// Neither is behind the lock below, and that is a statement rather than an
	// omission: the flush writes them, and the only other writer is the probe's
	// own callback, which a browser delivers from the event loop and never from
	// inside a call Go made. So the two cannot be in each other's middle.
	support oggSupport
	// codecs is the answer to the second probe: whether this browser's
	// WebCodecs AudioDecoder takes Vorbis, which is what decides whether a
	// streamed Clip decodes on a thread of the browser's or in wasm on this
	// one. It settles beside the Ogg probe and a prepare waits for both,
	// because both change which route a Clip takes.
	codecs  codecsSupport
	waiting []waiting

	// completedMu guards the two queues a callback or a goroutine appends to and
	// the flush drains. It is the only lock in the Adapter.
	completedMu sync.Mutex
	completed   []sound.Prepared
	// droppedRegions is the Loop Regions that were dropped, one per Clip that
	// declared one it could not have, waiting for the tick handler that holds a
	// Kernel.
	droppedRegions []error

	// failure is the one thing this Adapter has to say out loud: a page with no
	// Web Audio at all. It is left here for the tick handler to report once.
	failure error
}

// waiting is a prepare held until the Ogg probe settles: everything the dispatch
// will need, and nothing else.
type waiting struct {
	token   any
	encoded assets.Blob
	clip    *clipData
}

// newBackend opens the Engine's AudioContext and asks the browser the one
// question this Adapter asks it. It never fails: registration succeeds whether
// or not a Device exists, which is gfx's shape one Slot over, and a page with no
// Web Audio records a failure to report once and plays nothing.
func newBackend(cfg jssound.Config) *backend {
	b := &backend{
		clips:         make(map[sound.ClipID]*clipData),
		device:        sound.Device{Name: string(jssound.Name)},
		gestureTarget: gestureTarget(),
		timeConstant:  minTimeConstant,
		limit:         cfg.DecodedClipLimit,
	}
	audio, err := openContext(cfg.LatencyHint)
	if err != nil {
		b.failure = err
		return b
	}
	b.audio = audio
	if quantum := audio.quantum(); quantum > 0 {
		b.timeConstant = quantum
	}
	audio.watchState(b.stateChanged)
	audio.resumeOnGesture(b.gestureTarget, b.stateChanged)
	b.stateChanged()
	b.probeOgg()
	b.probeCodecs()
	return b
}

// gestureTarget is what the resume listener goes on: the document where there is
// one, and the global object otherwise. A gesture is a gesture whichever of them
// it bubbles to, and a host with neither simply never resumes - which is the
// same state as a player who never clicks, and is what Ready already says.
func gestureTarget() js.Value {
	if document := js.Global().Get("document"); document.Truthy() {
		return document
	}
	return js.Global()
}

// stateChanged refreshes what Device reports. It runs from the context's own
// statechange event rather than from a poll, which is what lets Device() stay a
// field read.
//
// Ready false covers absent, suspended and interrupted alike, because a game
// cannot tell them apart and does not need to: it draws its click-to-enable
// prompt on Ready being false, and the click that dismisses the prompt is the
// gesture that resumes the context.
func (b *backend) stateChanged() {
	if b.audio == nil || !b.audio.running() {
		b.device = sound.Device{Name: string(jssound.Name)}
		return
	}
	b.device = sound.Device{
		Ready:      true,
		Name:       string(jssound.Name),
		SampleRate: b.audio.sampleRate(),
		Channels:   outChannels,
		Latency:    b.audio.latency(),
	}
}

// Voices sizes the slot table, once, before any Emit. There is nothing else to
// size: a Voice's node graph is made when it starts, because Web Audio gives no
// way to restart a source node and a slot therefore cannot hold one waiting.
func (b *backend) Voices(n int) {
	if n <= 0 {
		n = 1
	}
	b.slots = make([]slot, n)
}

// Emit applies one tick's operations in the order the seam states them: the
// stops first, then the starts that reuse their slots, then the updates, then
// the destroys that the stops precede.
//
// The context clock is read once, at the top, and every operation in the batch
// is scheduled at that one instant. That is this Adapter's version of otosound
// handing a whole tick over atomically: two operations of one tick cannot land
// in two different render quanta, so a steal's stop and start are simultaneous
// rather than nearly so.
func (b *backend) Emit(batch *sound.Batch) {
	if b.audio == nil {
		// Nothing plays, but a release must still release: the Clip table is
		// the only thing holding the prepared value, and a page with no Device
		// is exactly where a game is least able to afford a leak.
		for _, id := range batch.Destroys {
			delete(b.clips, id)
		}
		return
	}
	now := b.audio.now()

	for _, at := range batch.Stops {
		if s := b.slot(at); s != nil && s.live != nil {
			s.live.stop(now)
			b.retire(s, now)
		}
	}
	for i := range batch.Starts {
		b.start(&batch.Starts[i], now)
	}
	for i := range batch.Updates {
		update := &batch.Updates[i]
		if s := b.slot(update.Slot); s != nil && s.live != nil {
			s.live.update(update.Params, now)
		}
	}
	// The streamed tier's whole scheduling, once per flush and on the same clock
	// read the batch was applied at: every live Voice is told where the playhead
	// has reached and every chunk its decode-ahead has finished goes onto the
	// context clock. A resident Voice's pump does nothing at all.
	//
	// It runs after the batch rather than before it, so a Voice that started in
	// this batch gets its first pump on the tick it started rather than waiting
	// for the next one, and a Voice stopped in this batch is never pumped again.
	for i := range b.slots {
		if live := b.slots[i].live; live != nil {
			live.pump(now)
		}
	}

	// A destroy frees nothing by itself here. The stops before it have already
	// told every source on that Clip to stop, and the browser keeps an
	// AudioBuffer alive for as long as a source is still playing it, so the
	// memory comes back when the last ramp has finished - which is the same
	// guarantee otosound spends an applied counter to make, bought here by the
	// fact that the graph and the samples are the same side of the seam.
	for _, id := range batch.Destroys {
		delete(b.clips, id)
	}
}

// start begins a Voice in a slot. It is also what a Seek is - a VoiceStart with
// an Offset, arriving on a slot that is already live - and both are one fresh
// source node started at a position in the Clip, which is what makes a Seek
// sample-accurate here.
//
// A start on a live slot retires what was there rather than reusing it, so the
// outgoing Voice ramps away on its own gains while the incoming one plays at
// full gain. That is the short crossfade a seek deserves, and it costs nothing
// that a shared gain stage would not have made impossible.
func (b *backend) start(at *sound.VoiceStart, now float64) {
	s := b.slot(at.Slot)
	if s == nil {
		return
	}
	if s.live != nil {
		s.live.stop(now)
		b.retire(s, now)
	}
	clip := b.clips[at.Clip]
	if clip == nil || !clip.audible() {
		// A Clip with no samples behind it is one prepared with no Web Audio.
		// The Voice exists above the seam either way, advances on sound's own
		// clock and ends on schedule; there is simply nothing to hear.
		return
	}
	s.live = startChain(b.audio, clip, at, now, b.timeConstant)
}

// retire moves the live chain aside for the one that replaces it, and takes the
// chain before that off the destination.
//
// The previous one is disconnected only once its ramp has certainly finished.
// Cutting a ramp short would be the click the ramp exists to remove, so a chain
// retired too recently is left to the browser's own collector instead: its
// source has been told to stop, nothing Go holds points at it, and a graph with
// no running source and no reference is collectable.
func (b *backend) retire(s *slot, now float64) {
	if s.retired != nil && now-s.retiredAt >= float64(declickTails)*b.timeConstant {
		s.retired.disconnect()
	}
	s.retired, s.retiredAt, s.live = s.live, now, nil
}

func (b *backend) slot(at sound.VoiceSlot) *slot {
	if int(at) < 0 || int(at) >= len(b.slots) {
		return nil
	}
	return &b.slots[at]
}

// Device reports the device as it is now. It is a field read, which is what lets
// sound poll it every flush.
func (b *backend) Device() sound.Device { return b.device }

// Prepare turns a Clip's encoded Ogg into an AudioBuffer. It always answers
// done=false: the browser's decode is a callback, the wasm fallback is a
// goroutine, and a Clip that behaved differently depending on which decoded it
// would make a game's timing depend on the browser it ran in.
//
// The headers are read here, synchronously, whichever decoder will follow. They
// are cheap - the identification header and the last page's granule position -
// and they are the only place the duration, the channels, the source rate and
// the Loop Region can come from: decodeAudioData surfaces none of them.
func (b *backend) Prepare(token any, encoded assets.Blob) (sound.PreparedClip, bool, error) {
	clip, err := readHeaders(encoded)
	if err != nil {
		return nil, false, err
	}
	held := waiting{token: token, encoded: encoded, clip: clip}
	if b.audio == nil {
		// No Web Audio: the Clip is its headers, exactly as nosound's is. It
		// still completes through the same queue and on the same tick boundary,
		// so a page with no Device runs a game's audio timeline identically to
		// one that has it. There is no tier either, because there is nothing to
		// decode into and nothing to schedule.
		b.completeUndecoded(held)
		return nil, false, nil
	}
	if b.probing() {
		b.waiting = append(b.waiting, held)
	} else {
		b.dispatch(held)
	}
	return nil, false, nil
}

// probing reports whether either capability probe is still outstanding. A Clip
// prepared now would be sent down a route the answer might have changed, which
// is the probe doing nothing, so it waits instead - at the cost of the first
// Clip of a session and nothing after it.
func (b *backend) probing() bool {
	return b.support == oggUnknown || b.codecs == codecsUnknown
}

// dispatch sends one prepare down one of three routes, and is the single place
// the tier is chosen.
//
// The tier is decided from the decoded size the headers already give - frames x
// channels x 4, computed before anything is decoded - which is what makes it a
// choice rather than a measurement. A Clip whose granule positions named no
// length has no such size and streams whatever the limit says, exactly as the
// spec states and exactly as otosound does; its frames are then counted on the
// goroutine the streamed route spawns.
//
// Nothing above this moves. readHeaders already produced the four facts the seam
// asks for, from the encoded bytes and with no decode, so the tier is a decision
// about how the samples will arrive and never about what the Clip is.
func (b *backend) dispatch(held waiting) {
	if held.clip.unmeasured || overLimit(b.limit, held.clip.decodedBytes()) {
		b.stream(held)
		return
	}
	if b.support == oggNative {
		b.decodeInBrowser(held)
		return
	}
	go func() {
		buffer, err := decodeInWasm(b.audio, held.encoded, held.clip)
		if err != nil {
			b.complete(sound.Prepared{Token: held.token, Err: err}, nil)
			return
		}
		held.clip.buffer = buffer
		b.complete(sound.Prepared{Token: held.token, Clip: held.clip}, held.clip)
	}()
}

// stream is the third route: nothing is decoded at load at all. The Clip keeps
// its bytes, a way to open decoders over them, and the filter its Voices will
// share, and a Voice gets its samples a chunk at a time from stream.go.
//
// The filter is built from the context's rate, which is not the Device rate
// being baked into what Prepare returns: an AudioContext's sampleRate is fixed
// for the life of the context, the samples themselves are converted per Voice at
// playback, and SampleRate still reports the file's own rate. What the Clip
// holds is a kernel, not audio.
func (b *backend) stream(held waiting) {
	clip := held.clip
	if clip.open == nil {
		clip.open = b.opener()
	}
	if ctxRate := b.audio.sampleRate(); ctxRate > 0 && ctxRate != clip.sourceRate {
		clip.filter = newSincFilter(clip.sourceRate, ctxRate)
	}
	b.completeUndecoded(held)
}

// completeUndecoded finishes a prepare that decodes nothing: a streamed Clip,
// and any Clip on a page with no Web Audio.
//
// A Clip whose length was never measured is measured here first, on a goroutine,
// because measuring means decoding and the only other place this could run is
// sound's flush. It is the one thing the streamed route waits for, and it waits
// only for a broken file.
func (b *backend) completeUndecoded(held waiting) {
	if !held.clip.unmeasured {
		b.complete(sound.Prepared{Token: held.token, Clip: held.clip}, held.clip)
		return
	}
	if held.clip.open == nil {
		held.clip.open = b.opener()
	}
	go func() {
		if err := held.clip.measure(); err != nil {
			b.complete(sound.Prepared{Token: held.token, Err: err}, nil)
			return
		}
		b.complete(sound.Prepared{Token: held.token, Clip: held.clip}, held.clip)
	}()
}

// decodeInBrowser is the path sound.md names for this Adapter: Prepare returns
// done=false and the decodeAudioData callback appends to the slice TakePrepared
// drains. One interface covers all three Adapters with no branch in sound.
func (b *backend) decodeInBrowser(held waiting) {
	b.audio.decode(held.encoded.Data(), func(buffer js.Value) {
		held.clip.buffer = buffer
		b.complete(sound.Prepared{Token: held.token, Clip: held.clip}, held.clip)
	}, func(message string) {
		b.complete(sound.Prepared{Token: held.token, Err: jssound.ErrDecodeRefused{Message: message}}, nil)
	})
}

// opener is how a streamed Clip on this browser gets its decoders, and is the
// whole of what the WebCodecs probe changes.
//
// It is read at Prepare rather than at each Voice, so a Clip keeps the route it
// was prepared under for its whole life. That is deliberate: the probe settles
// once and never moves, and a Clip whose Voices could disagree about which
// decoder they used would be two tiers inside one tier.
func (b *backend) opener() opener {
	if b.codecs == codecsPresent {
		return openStreamedVorbis
	}
	return openOgg
}

// probeOgg asks the browser whether it decodes Ogg Vorbis, once, by decoding the
// embedded micro-clip. Everything prepared before it answers waits for it.
func (b *backend) probeOgg() {
	b.audio.decode(probeClip,
		func(js.Value) { b.settleProbe(oggNative) },
		func(string) { b.settleProbe(oggWasm) })
}

// probeCodecs asks the browser whether its WebCodecs AudioDecoder takes Vorbis,
// with the same micro-clip's own headers. A page with no AudioDecoder at all
// answers here and now, so the wait this adds is a wait only on a browser that
// has one.
func (b *backend) probeCodecs() {
	probeCodecs(b.settleCodecs)
}

// settleProbe records the Ogg answer and releases whatever both probes were
// holding.
func (b *backend) settleProbe(support oggSupport) {
	b.support = support
	b.released()
}

// settleCodecs records the WebCodecs answer and does the same.
func (b *backend) settleCodecs(support codecsSupport) {
	b.codecs = support
	b.released()
}

// released dispatches everything that was waiting on the probes, once neither
// of them is outstanding.
func (b *backend) released() {
	if b.probing() {
		return
	}
	held := b.waiting
	b.waiting = nil
	for _, one := range held {
		b.dispatch(one)
	}
}

// complete queues one finished prepare for the next flush to drain, with the
// Loop Region notice that Clip owes beside it.
//
// The notice is queued here rather than where it was found because it belongs to
// a Clip that prepared: a Clip whose decode then failed never installs, and
// saying its loop tags are wrong would be a notice about something the game
// never got.
func (b *backend) complete(done sound.Prepared, clip *clipData) {
	b.completedMu.Lock()
	b.completed = append(b.completed, done)
	if clip != nil && clip.ignored != nil {
		b.droppedRegions = append(b.droppedRegions, clip.ignored)
	}
	b.completedMu.Unlock()
}

// TakePrepared returns the prepares that finished since the last call. It is the
// only poll that crosses back from a callback, and it crosses under a lock the
// audio thread could not reach even if it existed.
func (b *backend) TakePrepared() []sound.Prepared {
	b.completedMu.Lock()
	defer b.completedMu.Unlock()
	if len(b.completed) == 0 {
		return nil
	}
	taken := b.completed
	b.completed = nil
	return taken
}

// takeDroppedRegions returns the Loop Regions dropped since the last call and
// clears them, so each one is said once and by the one thing in this Extension
// that holds a Kernel.
//
// Draining is what makes it once per Clip. kernel.ReportErrorOnce wants a key
// that names the condition, and the condition here is a Clip - which the Adapter
// cannot name: sound's prepare token is opaque, and a ClipID does not exist
// until Install, a tick after the prepare that found this.
func (b *backend) takeDroppedRegions() []error {
	b.completedMu.Lock()
	defer b.completedMu.Unlock()
	if len(b.droppedRegions) == 0 {
		return nil
	}
	taken := b.droppedRegions
	b.droppedRegions = nil
	return taken
}

// Install mints the id sound names the Clip by afterwards. It is Install rather
// than Prepare that mints, so the handle comes into existence on sound's tick,
// where a destroy is guaranteed to pair with it.
func (b *backend) Install(prepared sound.PreparedClip) (sound.ClipID, error) {
	clip, ok := prepared.(*clipData)
	if !ok || clip == nil {
		return 0, errNotOurClip
	}
	b.lastID++
	b.clips[b.lastID] = clip
	return b.lastID, nil
}

// stop tears the graph down and closes the context, so a page that has navigated
// away from a game leaves nothing running and no listener installed.
func (b *backend) stop() {
	if b.audio == nil {
		return
	}
	now := b.audio.now()
	for i := range b.slots {
		s := &b.slots[i]
		if s.live != nil {
			s.live.stop(now)
			s.live.disconnect()
			s.live = nil
		}
		if s.retired != nil {
			s.retired.disconnect()
			s.retired = nil
		}
	}
	b.audio.close(b.gestureTarget)
	b.audio = nil
	b.device = sound.Device{Name: string(jssound.Name)}
}
