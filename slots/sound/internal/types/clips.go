package types

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
)

// ClipState is where a Clip is between being named and being playable.
type ClipState uint8

const (
	// ClipLoading is a Clip whose prepare is in flight. Voices recorded against
	// it exist, are addressable and are silent.
	ClipLoading ClipState = iota
	// ClipReady is a Clip a Voice can be started from.
	ClipReady
	// ClipFailed is a Clip that could not be read or prepared. It is terminal:
	// the entry holds the failure and only a release clears it.
	ClipFailed
)

// ClipInfo is what a game can ask about a Clip: the three facts a PreparedClip
// reports, and no id.
//
// There is no handle to a Clip here and there is not going to be one. Nothing a
// game must load or release is the requirement, and exporting an id invites a
// game to hold one; what gameplay legitimately wants of a Clip is a question,
// which is what ClipInfoOf is.
//
// Every field is zero until the Clip is resident, which is why the State beside
// it is not optional: a duration of zero and a state of ClipLoading are
// different answers to different questions, and a TextureDescr reporting
// Size() == 0, 0 forever is the lie this is here not to repeat.
type ClipInfo struct {
	// Duration is the Clip's length in seconds.
	Duration float32
	// Channels is 1 for mono and 2 for stereo. A stereo Clip is never
	// downmixed, so this is also what its gain matrix is shaped by.
	Channels int
	// SampleRate is the Clip's own rate, not the Device's. An Adapter may not
	// bake the Device rate into what it prepares, so this survives a Device
	// change.
	SampleRate int
}

// clipFacts is what sound knows about a Clip: its state, the id the Adapter
// minted for it, and the three facts a PreparedClip reports.
type clipFacts struct {
	state    ClipState
	id       ClipID
	duration float32
	channels int
	rate     int
}

// info renders the facts as the question answers them.
func (f clipFacts) info() ClipInfo {
	return ClipInfo{Duration: f.duration, Channels: f.channels, SampleRate: f.rate}
}

// clipEntry is one row of sound's own table, in front of the Library's.
//
// epoch is what makes a release recorded against an in-flight prepare cost
// nothing: the completion carries the epoch its prepare was started at, and a
// completion whose entry has moved on is dropped without calling anything. That
// is also why a prepared value must be garbage-collectable - there is no
// Destroy for something never installed.
type clipEntry struct {
	clipFacts
	epoch uint64
}

// clipLoader is sound's half of the Library's cache, and it is the identity.
// Load returns the bytes the Library already read; Default returns the zero
// Blob, which sound reads as "the read failed"; Free does nothing, because
// bytes are ordinary Go memory.
//
// Three lines, and in exchange sound inherits the Library's read, its
// report-once under the descriptor key, and its rule that the entry is the once
// - which is where terminal failure, report-once and the absence of a retry all
// come from together.
type clipLoader struct{}

func (clipLoader) Load(_ kernel.Kernel, data assets.Blob, _ clipParams, _ fs.FS, _ struct{}) assets.Blob {
	return data
}

func (clipLoader) Default(assets.Descr[clipParams], struct{}) assets.Blob { return assets.Blob{} }

func (clipLoader) Free(assets.Blob, struct{}) {}

// prepareToken is what crosses to the Adapter and comes back with a completion:
// the key of the entry the prepare belongs to, and the epoch that entry held
// when it started.
type prepareToken struct {
	key   ClipRef
	epoch uint64
}

// clipFailureKey names a Clip in the kernel's report-once table. It is its own
// type, so sound's clip failures own a namespace no other plugin's keys can
// collide with, and so a release can forget exactly one of them.
type clipFailureKey ClipRef

// Clips is sound's own clip table, sitting in front of the Library's Get.
//
// The surface splits where libs/assets' own rule puts it: the Library holds the
// encoded bytes and gives sound the storage read, report-once, terminal failure
// and Free, while the pending state, the epochs, Prepare / TakePrepared /
// Install and the ClipID are sound's own, here.
type Clips struct {
	cache   *assets.Cache[clipParams, struct{}, assets.Blob]
	entries map[ClipRef]*clipEntry
	epoch   uint64
	// destroyed is the ids a release took out of the table this tick, waiting
	// for the batch that carries them. It keeps its capacity between ticks, and
	// it is a list here rather than a Destroy call at the release because the
	// batch is the only route with an order: the destroy has to arrive after
	// the stops of the Voices the same release ended.
	destroyed []ClipID
}

// NewClips builds an empty table over a cache whose loader is the identity.
func NewClips() *Clips {
	return &Clips{
		cache:   assets.New[clipParams, struct{}, assets.Blob](clipLoader{}),
		entries: map[ClipRef]*clipEntry{},
	}
}

// resolve returns what sound knows about a Clip, reading and preparing it when
// the table has no entry for it.
//
// The read is synchronous, here, on the tick the Play that needs it was
// recorded. That is not an implementation accident left implicit: it is the one
// place sound can stall a frame. storage gives no other option, since its
// resource must not be retained past the handler that declared it, so there is
// no filesystem to hand a goroutine. The prepare is what moves off the tick.
func (c *Clips) resolve(k kernel.Kernel, fsys fs.FS, backend Backend, ref ClipRef) clipFacts {
	key := ref.key()
	if entry, ok := c.entries[key]; ok {
		return entry.clipFacts
	}
	c.epoch++
	entry := &clipEntry{epoch: c.epoch}
	c.entries[key] = entry

	encoded := c.cache.Get(k, ref.descr(), fsys, struct{}{})
	if encoded.Len() == 0 {
		// A path that would not read is the Library's own report, already made
		// once under its descriptor key; saying it again here would double
		// every missing sound. Bytes that are not there are nobody else's to
		// report.
		entry.state = ClipFailed
		if key.name == "" {
			k.ReportErrorOnce(clipFailureKey(key), ErrClipUnreadable{Clip: ref.describe()})
		}
		return entry.clipFacts
	}

	prepared, done, err := backend.Prepare(prepareToken{key: key, epoch: entry.epoch}, encoded)
	switch {
	case err != nil:
		c.fail(k, entry, key, ErrClipFailed{Clip: key.describe(), Err: err})
	case done:
		c.install(k, backend, entry, key, prepared)
	}
	return entry.clipFacts
}

// preload reads and prepares a Clip nothing is playing yet, so that the frame
// which eats the read is a loading screen rather than the first shot fired.
//
// It is resolve with the answer thrown away, and that is the whole of it: a
// Preload and a Play name the same entry, so a Play arriving later finds the
// work already done or already in flight, and a Preload of a Clip the table
// already holds costs a map lookup.
//
// What it promises is residency and not a free Play. For a Clip short enough to
// be decoded whole the next Play is free; for a longer one the bytes are read
// and the headers parsed here, but the first Play still opens a decoder of its
// own and is silent until that Voice's read-ahead primes. A game cannot tell
// which tier it got - that is the point of the tier being the Adapter's - so
// the promise that is always true is the weaker one.
func (c *Clips) preload(k kernel.Kernel, fsys fs.FS, backend Backend, ref ClipRef) {
	c.resolve(k, fsys, backend, ref)
}

// release drops one Clip from the table and queues its destroy.
//
// It does not stop the Voices playing it - the flush does that first, in the
// same tick - and it does not free anything below the seam: the id goes into
// destroyed, the next batch carries it after those stops, and the Adapter frees
// it once its Mixer has passed that batch.
//
// Releasing a Clip whose prepare is in flight costs nothing: the entry goes, so
// the completion arrives to find no entry at its epoch and is dropped with
// nothing called - which is why a prepared value has to be garbage-collectable.
//
// It also forgets the Clip's failure, here and in the Library both, which is
// what makes a release the one thing that clears a terminal failure: a path
// that failed, was fixed and was named again can be read, and can speak.
func (c *Clips) release(k kernel.Kernel, ref ClipRef) {
	key := ref.key()
	entry, ok := c.entries[key]
	if !ok {
		return
	}
	delete(c.entries, key)
	c.forget(k, key, entry)
}

// releaseAll empties the table, which is what a teardown call means. A game
// that wants one track to bridge a transition releases per Clip and keeps the
// bridging one, or starts the bridge after this.
//
// It is the per-Clip release over every entry and not the Library's FreeAll,
// because the two tables are in lockstep by construction: every cache.Get
// sound makes is made by resolve, after resolve has put an entry in front of
// it, so an entry leaving is the only way a cached Blob ever needs to leave.
func (c *Clips) releaseAll(k kernel.Kernel) {
	for key, entry := range c.entries {
		c.forget(k, key, entry)
	}
	clear(c.entries)
}

// forget is what one entry leaving the table costs: its id queued for the next
// batch, its bytes dropped from the Library, and its failure forgotten.
//
// An entry that never reached ClipReady has no id, so there is nothing to
// destroy - a loading entry's prepare is dropped by its epoch and a failed one
// never installed anything.
func (c *Clips) forget(k kernel.Kernel, key ClipRef, entry *clipEntry) {
	if entry.state == ClipReady && entry.id != 0 {
		c.destroyed = append(c.destroyed, entry.id)
	}
	c.cache.Free(k, key.descr(), struct{}{})
	k.ForgetReportedError(clipFailureKey(key))
}

// collect moves this tick's destroys into the batch and clears the list. It
// runs after the Voices have been collected, so every stop a release caused is
// already in the same batch, ahead of the destroy that follows it.
func (c *Clips) collect(batch *Batch) {
	batch.Destroys = append(batch.Destroys, c.destroyed...)
	c.destroyed = c.destroyed[:0]
}

// info answers what a game can ask about a Clip, and starts nothing.
//
// A Clip the table has no entry for reports ClipLoading with zero facts, and
// that is honest rather than convenient: nothing failed, nothing is resident,
// and asking is not what makes a Clip load. It does mean a game that only ever
// asks is told ClipLoading forever - Preload is the verb that changes the
// answer, and a question that loaded would be the second read sound does not
// have.
func (c *Clips) info(ref ClipRef) (ClipInfo, ClipState) {
	facts := c.lookup(ref)
	return facts.info(), facts.state
}

// lookup answers what sound already knows about a Clip, and starts nothing. It
// reads sound's own table and never the Library, because the Library's Get is
// the only read it has and it loads on a miss.
func (c *Clips) lookup(ref ClipRef) clipFacts {
	if entry, ok := c.entries[ref.key()]; ok {
		return entry.clipFacts
	}
	return clipFacts{}
}

// drain installs every prepare that finished since the last flush, and drops
// the completions whose entries are gone.
//
// It runs first in the flush, which is what makes a release recorded against an
// in-flight prepare cost nothing: by the time the completion arrives its entry
// is gone, so no ClipID is ever minted only to be destroyed.
func (c *Clips) drain(k kernel.Kernel, backend Backend) {
	for _, done := range backend.TakePrepared() {
		token, ok := done.Token.(prepareToken)
		if !ok {
			continue
		}
		entry := c.entries[token.key]
		if entry == nil || entry.epoch != token.epoch || entry.state != ClipLoading {
			continue
		}
		if done.Err != nil {
			c.fail(k, entry, token.key, ErrClipFailed{Clip: token.key.describe(), Err: done.Err})
			continue
		}
		c.install(k, backend, entry, token.key, done.Clip)
	}
}

// install mints the Clip's id on sound's tick, where a Destroy is guaranteed to
// pair with it, and records the three facts sound needs to end a Voice on time.
func (c *Clips) install(k kernel.Kernel, backend Backend, entry *clipEntry, key ClipRef, prepared PreparedClip) {
	if prepared == nil {
		c.fail(k, entry, key, ErrClipNotPrepared{Clip: key.describe()})
		return
	}
	id, err := backend.Install(prepared)
	if err != nil {
		c.fail(k, entry, key, ErrClipFailed{Clip: key.describe(), Err: err})
		return
	}
	if id == 0 {
		c.fail(k, entry, key, ErrClipNotPrepared{Clip: key.describe()})
		return
	}
	entry.clipFacts = clipFacts{
		state:    ClipReady,
		id:       id,
		duration: prepared.Duration(),
		channels: prepared.Channels(),
		rate:     prepared.SampleRate(),
	}
}

// fail records a terminal failure and reports it once. There is no retry and no
// try-again: every Clip failure is terminal - a bad path, a file that is not
// Ogg Vorbis, a corrupt stream - and only a release clears it.
func (c *Clips) fail(k kernel.Kernel, entry *clipEntry, key ClipRef, err error) {
	entry.clipFacts = clipFacts{state: ClipFailed}
	k.ReportErrorOnce(clipFailureKey(key), err)
}
