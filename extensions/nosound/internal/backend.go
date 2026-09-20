package internal

import (
	"bytes"

	"github.com/dvoyni/cog/extensions/nosound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/jfreymuth/oggvorbis"
)

// preparedClip is everything nosound keeps of a Clip: the three facts the
// contract must be able to report, and no samples. Which kind of prepared Clip
// an Adapter makes is its own business, and this one's kind is "the header".
type preparedClip struct {
	duration float32
	channels int
	rate     int
}

func (c preparedClip) Duration() float32 { return c.duration }

func (c preparedClip) Channels() int { return c.channels }

func (c preparedClip) SampleRate() int { return c.rate }

// LoopRegion reports no region, so a looping Voice loops the whole Clip -
// which is what every Clip with no LOOPSTART tag says, so nothing moves.
// Reading the tag out of the Vorbis comment header is issue 483.
func (c preparedClip) LoopRegion() m.Maybe[sound.LoopRegion] {
	return m.Maybe[sound.LoopRegion]{}
}

// backend is the silent Adapter: it accepts every start, update, stop and
// destroy and keeps none of them, and answers the two questions sound polls.
//
// It takes no lock and needs none. sound calls every method of a Backend from
// its own flush, which holds a write lock on all four of its resources, so the
// calls are serialized against each other by construction and the id counter
// below is touched from one goroutine at a time.
type backend struct {
	// device is what Device reports, built once at registration because none of
	// it can change: nothing is opened, so nothing can be lost.
	device sound.Device
	// installed counts the ids handed out. It is a counter rather than a
	// record: an id must be non-zero and must not repeat, and nothing else
	// about a Clip is kept.
	installed uint32
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
	return preparedClip{
		duration: float32(frames) / float32(rate),
		channels: channels,
		rate:     rate,
	}, true, nil
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
