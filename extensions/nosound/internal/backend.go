package internal

import (
	"bytes"
	"sync"

	"github.com/dvoyni/cog/extensions/nosound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/jfreymuth/oggvorbis"
)

// preparedClip is everything nosound keeps of a Clip: the four facts the
// contract must be able to report, and no samples. Which kind of prepared Clip
// an Adapter makes is its own business, and this one's kind is "the header".
type preparedClip struct {
	duration float32
	channels int
	rate     int
	region   m.Maybe[sound.LoopRegion]
}

func (c preparedClip) Duration() float32 { return c.duration }

func (c preparedClip) Channels() int { return c.channels }

func (c preparedClip) SampleRate() int { return c.rate }

// LoopRegion reports the span the Clip's own Vorbis comments declare, and
// absent when it declares none - which means the whole Clip. It comes out of
// the same header pass that already read the duration, the channels and the
// rate, so nosound and otosound cannot disagree about a Clip.
func (c preparedClip) LoopRegion() m.Maybe[sound.LoopRegion] { return c.region }

// backend is the silent Adapter: it accepts every start, update, stop and
// destroy and keeps none of them, and answers the two questions sound polls.
//
// sound calls every method of a Backend from its own flush, which holds a write
// lock on all four of its resources, so the calls are serialized against each
// other by construction and the id counter below is touched from one goroutine
// at a time. The one lock here guards the one thing something other than the
// flush reads: the Loop Regions a Clip declared and could not have, which the
// subscription that holds a Kernel drains.
type backend struct {
	// device is what Device reports, built once at registration because none of
	// it can change: nothing is opened, so nothing can be lost.
	device sound.Device
	// installed counts the ids handed out. It is a counter rather than a
	// record: an id must be non-zero and must not repeat, and nothing else
	// about a Clip is kept.
	installed uint32

	// droppedMu guards droppedRegions, which is the one thing nosound has to
	// say out loud and the one place a second goroutine reaches it: Prepare
	// writes it from sound's flush and the subscription that holds a Kernel
	// drains it, and the two declare no resource in common to be serialized by.
	droppedMu      sync.Mutex
	droppedRegions []error
}

func newBackend(sampleRate int) *backend {
	return &backend{device: sound.Device{
		Ready:      true,
		Name:       string(nosound.Name),
		SampleRate: sampleRate,
		Channels:   2,
	}}
}

// Voices is told how many slots exist and keeps the number nowhere: there is no
// table to size when there is nothing to mix.
func (b *backend) Voices(int) {}

// Emit accepts one tick's operations and plays none of them.
func (b *backend) Emit(*sound.Batch) {}

// Device reports a working Device that plays nothing. Ready is true, and
// Latency is zero because nothing is buffered.
func (b *backend) Device() sound.Device { return b.device }

// Prepare reads the Ogg headers and the stream length, and decodes no samples.
//
// It parses a bytes.Reader over the bytes it was handed rather than whatever
// storage opened, because Length reads the last page's granule position and so
// needs an io.Seeker, which storage.FileSystem.Open does not promise. That is
// O(file size) in I/O and O(1) in CPU: the header cost is flat, about half a
// millisecond whatever the Clip's length, which is the property that matters -
// a CI suite's cost stops scaling with the audio it names.
//
// "Header only" is constant work rather than zero samples: NewReader decodes
// the first audio packet, because Vorbis data need not start at position zero.
// This says so rather than promising more than the library does.
//
// It is done immediately, so nothing ever appears in TakePrepared.
func (b *backend) Prepare(_ any, encoded assets.Blob) (sound.PreparedClip, bool, error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(encoded.Data()))
	if err != nil {
		return nil, false, nosound.ErrNotOggVorbis{Err: err}
	}
	rate, channels := reader.SampleRate(), reader.Channels()
	if rate <= 0 || channels <= 0 {
		return nil, false, nosound.ErrNoStreamFormat{SampleRate: rate, Channels: channels}
	}
	frames := reader.Length()
	if frames <= 0 {
		return nil, false, nosound.ErrNoStreamLength{}
	}
	frames, region, dropped := clipBounds(encoded, rate, frames, frames)
	if dropped != nil {
		b.droppedMu.Lock()
		b.droppedRegions = append(b.droppedRegions, dropped)
		b.droppedMu.Unlock()
	}
	return preparedClip{
		duration: float32(frames) / float32(rate),
		channels: channels,
		rate:     rate,
		region:   region,
	}, true, nil
}

// takeDroppedRegions returns the Loop Regions dropped since the last call and
// clears them, so each one is said once and by the one thing in this Extension
// that holds a Kernel. Draining is what makes it once per Clip: a prepare runs
// once per entry in sound's table, and the Adapter has no name for a Clip to
// key kernel.ReportErrorOnce by.
func (b *backend) takeDroppedRegions() []error {
	b.droppedMu.Lock()
	defer b.droppedMu.Unlock()
	if len(b.droppedRegions) == 0 {
		return nil
	}
	taken := b.droppedRegions
	b.droppedRegions = nil
	return taken
}

// TakePrepared returns nothing, because Prepare is always done.
func (b *backend) TakePrepared() []sound.Prepared { return nil }

// Install mints the id sound names the Clip by afterwards. The value behind it
// is a header and is garbage-collectable, so a Clip released while its prepare
// was in flight leaves nothing unfreed.
func (b *backend) Install(sound.PreparedClip) (sound.ClipID, error) {
	b.installed++
	return sound.ClipID(b.installed), nil
}
