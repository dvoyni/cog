package types

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
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

// clipFacts is what sound knows about a Clip: its state, the id the Adapter
// minted for it, and the four facts a PreparedClip reports.
type clipFacts struct {
	state    ClipState
	id       ClipID
	duration float32
	channels int
	rate     int
	// region is the Clip's Loop Region, in seconds, and is absent when the Clip
	// declares none - which means the whole Clip. sound keeps it for two
	// reasons of its own: a looping Voice's playhead wraps at the loop end
	// rather than at the duration, and a Seek past the end wraps to the loop
	// start rather than to zero.
	region m.Maybe[LoopRegion]
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
// pair with it, and records the four facts sound needs to end a Voice on time
// and to wrap a looping one where its Clip says.
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
		region:   prepared.LoopRegion(),
	}
}

// fail records a terminal failure and reports it once. There is no retry and no
// try-again: every Clip failure is terminal - a bad path, a file that is not
// Ogg Vorbis, a corrupt stream - and only a release clears it.
func (c *Clips) fail(k kernel.Kernel, entry *clipEntry, key ClipRef, err error) {
	entry.clipFacts = clipFacts{state: ClipFailed}
	k.ReportErrorOnce(clipFailureKey(key), err)
}
