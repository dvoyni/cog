//go:build !js

package internal

import (
	"bytes"
	"cmp"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/jfreymuth/oggvorbis"
)

const (
	// bytesPerSample is one decoded sample as the Mixer holds it, which is what
	// the decoded size a Clip is measured against is counted in.
	bytesPerSample = 4
	// defaultDecodedClipLimit is the limit a Config that names none gets: the
	// largest decoded size a Clip may have and still be held resident. At
	// 512 KiB that is 2.97 s mono 44.1 kHz, 1.49 s stereo 44.1 kHz, 1.37 s
	// stereo 48 kHz.
	//
	// It sits deliberately above the 137 KB a decoder costs, which is where
	// streaming starts to save memory at all. Between the two, memory is
	// knowingly spent to make Play free: a Clip under the limit is already
	// samples, so starting a Voice on it is an index rather than 460 us of
	// decoder.
	defaultDecodedClipLimit = 512 << 10
	// neverStream and alwaysStream are the two sentinels DecodedClipLimit takes
	// beside a size in bytes. One field with sentinels rather than a second
	// bool beside it, because two fields can disagree and one cannot.
	neverStream  = -1
	alwaysStream = -2
)

// clipData is a prepared Clip, in either tier.
//
// It is one type for both because both tiers report the same four facts -
// duration, channels, rate and loop region - and sound must not be able to tell
// which it got. A second type would be a second thing for the Port to carry and
// the day something switched on it would be the day the tier stopped being the
// Adapter's business.
//
// A resident Clip is the whole thing decoded once, interleaved, and already
// converted to the rate the Mixer produces, so that the device thread only ever
// interpolates for a Voice's Rate and never for a base rate. A streamed Clip is
// the encoded bytes and a way to open decoders against them; its Voices each
// get a decoder and a read-ahead ring, and the device thread copies from the
// ring exactly as it indexes a resident Clip's samples.
//
// The four facts it reports are the source's rather than the device's: a game
// asking a Clip its rate is asking about the file, and an Adapter that answered
// with the device's would make one Clip two cache entries the day the device
// changed. Nothing above the seam can tell that the samples were converted, and
// nothing above the seam can tell whether there are any samples yet.
//
// It is garbage-collectable, which is the Port's rule: a Clip released while
// its prepare was in flight has its completion dropped with nothing called, so
// anything needing explicit release would never be freed. That holds for the
// streamed tier too - what it adds is a Blob, which is a pointer and a length
// into the run the asset cache already holds.
type clipData struct {
	// samples are interleaved, channels per frame, at rate. A streamed Clip has
	// none: its frames arrive in a Voice's ring.
	samples []float32
	// frames is the Clip's length in converted frames - len(samples)/channels
	// for a resident Clip, and what the source will become for a streamed one.
	frames int
	// channels is the source's, one or two. The 2x2 gain matrix addresses no
	// more than two, so a third is refused in Prepare rather than truncated.
	channels int
	// rate is the device rate the samples are, or will be, converted to.
	rate int
	// sourceRate is what the file said, and is what SampleRate reports.
	sourceRate int
	// sourceFrames is the file's length in its own frames, which is what a
	// read-ahead seeks and loops in.
	sourceFrames int64
	// duration is the source's length in seconds, computed the way nosound
	// computes it so that the two Adapters report one Clip identically.
	duration float32
	// region is the Clip's Loop Region, read off its Vorbis comments by
	// clipBounds. Absent is what a Clip with no tag says, and means the whole
	// Clip, so a file that carries no tags loops exactly as it always did.
	region m.Maybe[sound.LoopRegion]
	// ignored is the Loop Region this Clip declared and could not have: a tag
	// that did not parse, or a span that is not inside the Clip it sits in. The
	// region is dropped whole and never clamped, and this is what says so out
	// loud - a clamped loop sounds like a working loop with the wrong loop
	// point, which is the hardest audio bug there is to attribute.
	//
	// It rides on the Clip rather than being reported where it is found,
	// because it is found on the goroutine Prepare spawns and a report needs a
	// Kernel, which only the tick holds. The tick queues it once, when the
	// prepare completes, which is once per Clip: a prepare runs once per entry
	// in sound's table.
	ignored error

	// encoded is the Clip's own bytes, retained by a streamed Clip and dropped
	// by a resident one the moment it has samples. Retaining costs nothing: a
	// Blob is a pointer and a length, so it references the same run the asset
	// cache holds rather than copying it.
	encoded assets.Blob
	// filter is the resampler this Clip's frames are converted with, shared by
	// every read-ahead on it and nil when the file is already at the device's
	// rate. A resident Clip does not keep one: it was converted once, in
	// Prepare, and the filter went with the encoded bytes.
	filter *sincFilter
	// open is how a Voice on this Clip gets a decoder, and is nil on a resident
	// Clip - which is what "is this Clip streamed" asks. It is a field rather
	// than a package function so that a test can stream a Clip it generated
	// with no Ogg anywhere in it, including one whose decoder panics if the
	// device thread ever reaches it.
	open opener
}

func (c *clipData) Duration() float32 { return c.duration }

func (c *clipData) Channels() int { return c.channels }

func (c *clipData) SampleRate() int { return c.sourceRate }

func (c *clipData) LoopRegion() m.Maybe[sound.LoopRegion] { return c.region }

// streams reports whether this Clip's Voices read through a decoder rather than
// out of a shared buffer.
func (c *clipData) streams() bool { return c.open != nil }

// loopBounds is the span in converted frames a looping resident Voice repeats
// between: the Clip's Loop Region when it declares one, and the whole Clip
// otherwise. It is computed once, when a Voice starts, so the device thread
// never touches a Maybe or a seconds-to-frames conversion inside a block.
func (c *clipData) loopBounds() (start, end float64) {
	end = float64(c.frames)
	region, ok := c.region.Get()
	if !ok {
		return 0, end
	}
	start = math.Round(float64(region.Start) * float64(c.rate))
	stop := math.Round(float64(region.End) * float64(c.rate))
	if stop > start && stop <= end {
		end = stop
	}
	if start < 0 || start >= end {
		return 0, float64(c.frames)
	}
	return start, end
}

// sourceLoopBounds is the same span in the source's own frames, which is what a
// read-ahead seeks in: it loops by seeking the decoder, so the wrap happens
// before the conversion rather than after it.
func (c *clipData) sourceLoopBounds() (start, end int64) {
	end = c.sourceFrames
	region, ok := c.region.Get()
	if !ok {
		return 0, end
	}
	start = int64(math.Round(float64(region.Start) * float64(c.sourceRate)))
	stop := int64(math.Round(float64(region.End) * float64(c.sourceRate)))
	if stop > start && stop <= end {
		end = stop
	}
	if start < 0 || start >= end {
		return 0, c.sourceFrames
	}
	return start, end
}

// sourceFrame is where a VoiceStart's Offset lands in the source's own frames,
// clamped into the Clip so that an offset past the end reads the end rather
// than seeking off the stream.
func (c *clipData) sourceFrame(offset time.Duration) int64 {
	at := int64(offset.Seconds() * float64(c.sourceRate))
	if at < 0 {
		return 0
	}
	if at > c.sourceFrames {
		return c.sourceFrames
	}
	return at
}

// The three Vorbis comments a Loop Region is declared with, in sample frames.
// They are the de-facto convention game-music tooling already writes, which is
// the whole point of choosing them: a composer's export is the authoring
// surface, and no Go code has to agree with a file it cannot see.
//
// They are matched case-insensitively, because a Vorbis comment's field name
// is case-insensitive by the format's own definition and the tools in the wild
// disagree about it.
const (
	loopStartTag  = "LOOPSTART"
	loopLengthTag = "LOOPLENGTH"
	loopEndTag    = "LOOPEND"
)

// clipBounds is the Adapter's one pass over the headers for the two facts that
// say where a Clip ends and where it repeats: the true final frame its granule
// positions name, and the Loop Region its Vorbis comments declare, in seconds.
//
// The two are one function because they are one fact looked at twice. A decoded
// buffer may carry a final packet's padding, so the frame count a decoder hands
// back is not always the stream's length; an absent region has to mean
// loop-to-the-granule-end and never loop-to-the-padded-buffer-end, and a
// LOOPEND is only in range against that same corrected count. Resolved apart,
// the region would be checked against a length the Clip does not have.
//
// sound sees neither input. What crosses the seam is a duration and a span,
// both in seconds, and no signature above here names a sample.
//
// granule is the last page's granule position, which is zero on a stream whose
// pages carry none; decoded is however many frames were actually produced or
// counted. A malformed region is reported rather than clamped and the Clip
// loops whole, which is the one thing a caller of this must not paper over.
func clipBounds(encoded assets.Blob, sourceRate int, granule, decoded int64) (int64, m.Maybe[sound.LoopRegion], error) {
	var none m.Maybe[sound.LoopRegion]
	frames := decoded
	if granule > 0 && granule < frames {
		frames = granule
	}

	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(encoded.Data()))
	if err != nil {
		// The identification header parsed and this one did not, so the file is
		// damaged in a way that costs it only its tags. The Clip still plays.
		return frames, none, otosound.ErrLoopRegionIgnored{Frames: frames, Err: err}
	}
	start, hasStart, startErr := loopTag(header.Comments, loopStartTag)
	length, hasLength, lengthErr := loopTag(header.Comments, loopLengthTag)
	stop, hasEnd, endErr := loopTag(header.Comments, loopEndTag)
	if err := cmp.Or(startErr, lengthErr, endErr); err != nil {
		return frames, none, otosound.ErrLoopRegionIgnored{Frames: frames, Err: err}
	}
	if !hasStart && !hasLength && !hasEnd {
		return frames, none, nil
	}

	// LOOPSTART alone is the case this whole feature is named for - an intro
	// that runs into a loop that carries on to the end of the file - so the end
	// a Clip does not name is the Clip's own end rather than nothing at all.
	// LOOPLENGTH wins over LOOPEND where a file carries both, which is the
	// order the table in the spec states them in.
	switch {
	case hasLength:
		stop = start + length
	case !hasEnd:
		stop = frames
	}
	if start < 0 || stop <= start || stop > frames {
		return frames, none, otosound.ErrLoopRegionIgnored{Start: start, End: stop, Frames: frames}
	}
	return frames, m.Some(sound.LoopRegion{
		Start: float32(float64(start) / float64(sourceRate)),
		End:   float32(float64(stop) / float64(sourceRate)),
	}), nil
}

// loopTag reads one loop comment in sample frames, and reports whether the file
// carried it at all. A Vorbis comment is NAME=value and a file may repeat a
// name; the first one wins, so a re-tagged file plays the way the tool that
// re-tagged it last meant rather than the way the one before it did.
func loopTag(comments []string, name string) (int64, bool, error) {
	for _, comment := range comments {
		at := strings.IndexByte(comment, '=')
		if at < 0 || !strings.EqualFold(comment[:at], name) {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(comment[at+1:]), 10, 64)
		if err != nil {
			return 0, true, err
		}
		return value, true, nil
	}
	return 0, false, nil
}

// prepare turns encoded Ogg Vorbis into a Clip in whichever tier its size asks
// for. It runs on the goroutine Prepare spawns and never on the tick or the
// device thread, which is the first obligation an Adapter owes: nothing decodes
// on the thread that fills the device buffer, and nothing decodes on the thread
// the game runs on either.
//
// The tier is chosen from the decoded size rather than the encoded size,
// because decoded size is what costs memory - and it is computable before
// decoding anything, as frames x channels x 4, from the identification header
// and the end granule position. GetLength reads exactly those two and no setup
// header, so choosing the tier costs microseconds rather than the 460 the
// decoder it might not need would have.
//
// It reads over a bytes.Reader on the bytes it was handed rather than whatever
// storage opened, because the length is read from the stream's granule
// positions and so needs an io.Seeker, which storage.FileSystem.Open does not
// promise.
func prepare(encoded assets.Blob, deviceRate, limit int) (*clipData, error) {
	length, format, err := oggvorbis.GetLength(bytes.NewReader(encoded.Data()))
	if err != nil {
		return nil, otosound.ErrNotOggVorbis{Err: err}
	}
	if format.SampleRate <= 0 || format.Channels <= 0 || format.Channels > 2 {
		return nil, otosound.ErrNoStreamFormat{SampleRate: format.SampleRate, Channels: format.Channels}
	}
	// A length of zero is a stream whose granule positions say nothing - a
	// truncated file - and has no computable decoded size, so it streams
	// whatever the limit says.
	if length > 0 && !overLimit(limit, length*int64(format.Channels)*bytesPerSample) {
		return decode(encoded, deviceRate, length)
	}
	return retain(encoded, deviceRate, format.SampleRate, format.Channels, length)
}

// overLimit reports whether a decoded size is more than a Config will hold
// resident. Zero is the default limit, -1 refuses to stream anything and -2
// streams everything.
func overLimit(limit int, decoded int64) bool {
	switch limit {
	case alwaysStream:
		return true
	case neverStream:
		return false
	case 0:
		return decoded > defaultDecodedClipLimit
	default:
		return decoded > int64(limit)
	}
}

// decode is the resident tier: the whole Clip decoded once and converted to the
// device rate, with the encoded bytes dropped behind it - nothing holds them,
// because nothing will read them again.
//
// granule is the last page's granule position, as prepare already read it. The
// decoded buffer is trimmed to it rather than believed: a final packet may
// carry padding past the stream's true end, and a Clip that looped to the end
// of the buffer rather than to the end of the stream would repeat a fraction of
// a block of silence every time round.
func decode(encoded assets.Blob, deviceRate int, granule int64) (*clipData, error) {
	samples, format, err := oggvorbis.ReadAll(bytes.NewReader(encoded.Data()))
	if err != nil {
		return nil, otosound.ErrNotOggVorbis{Err: err}
	}
	if format.SampleRate <= 0 || format.Channels <= 0 || format.Channels > 2 {
		return nil, otosound.ErrNoStreamFormat{SampleRate: format.SampleRate, Channels: format.Channels}
	}
	bounded, region, ignored := clipBounds(encoded, format.SampleRate, granule, int64(len(samples)/format.Channels))
	frames := int(bounded)
	if frames <= 0 {
		return nil, otosound.ErrNoStreamLength{}
	}
	samples = samples[:frames*format.Channels]

	clip := &clipData{
		channels:     format.Channels,
		rate:         deviceRate,
		sourceRate:   format.SampleRate,
		sourceFrames: int64(frames),
		duration:     float32(frames) / float32(format.SampleRate),
		region:       region,
		ignored:      ignored,
	}
	if format.SampleRate == deviceRate {
		clip.samples, clip.frames = samples, frames
		return clip, nil
	}
	clip.samples, clip.frames = convertRate(samples, format.Channels, frames, format.SampleRate, deviceRate)
	if clip.frames <= 0 {
		return nil, otosound.ErrNoStreamLength{}
	}
	return clip, nil
}

// retain is the streamed tier: the encoded bytes kept, the filter its Voices
// will share built once, and not a sample decoded until a Voice asks for one.
//
// A length of zero is counted rather than believed. The spec streams such a
// Clip because there is no decoded size to compare, but a Clip that reported no
// duration would be worse than that: a zero duration makes the playhead fiction
// and silently removes ReasonFinished from every Voice that names the Clip, so
// what a length of zero costs is one scan whose samples are thrown away, and a
// stream that truly holds no frames is the terminal failure it always was.
func retain(encoded assets.Blob, deviceRate, sourceRate, channels int, length int64) (*clipData, error) {
	if length <= 0 {
		counted, err := scanLength(encoded, channels)
		if err != nil {
			return nil, otosound.ErrNotOggVorbis{Err: err}
		}
		if counted <= 0 {
			return nil, otosound.ErrNoStreamLength{}
		}
		length = counted
	}
	length, region, ignored := clipBounds(encoded, sourceRate, length, length)

	clip := &clipData{
		channels:     channels,
		frames:       int(length),
		rate:         deviceRate,
		sourceRate:   sourceRate,
		sourceFrames: length,
		duration:     float32(length) / float32(sourceRate),
		region:       region,
		ignored:      ignored,
		encoded:      encoded,
		open:         openOgg,
	}
	if sourceRate != deviceRate {
		clip.filter = newSincFilter(sourceRate, deviceRate)
		clip.frames = int(float64(length) * clip.filter.ratio)
	}
	if clip.frames <= 0 {
		return nil, otosound.ErrNoStreamLength{}
	}
	return clip, nil
}

// scanLength counts a stream's frames by decoding it and throwing the samples
// away. It is the only path in the Adapter that decodes a whole Clip it will
// not keep, and it runs only for a file whose granule positions carry no
// length, which is a broken one.
func scanLength(encoded assets.Blob, channels int) (int64, error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(encoded.Data()))
	if err != nil {
		return 0, err
	}
	scratch := make([]float32, decodeChunk*channels)
	var frames int64
	for {
		read, err := reader.Read(scratch)
		frames += int64(read / channels)
		if err == io.EOF {
			return frames, nil
		}
		if err != nil {
			return frames, err
		}
		if read == 0 {
			return frames, nil
		}
	}
}
