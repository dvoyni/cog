package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/sound"
)

// The fixture clip's own facts, read off the file it was cut to. They are
// written down rather than computed so that a change to the file fails here
// rather than quietly moving every tick these tests count.
const (
	clipFrames   = 48704
	clipRate     = 44100
	clipChannels = 2
)

// clipDuration is the fixture's length in seconds as sound will hold it:
// float32, computed the way Prepare computes it.
const clipDuration = float32(clipFrames) / float32(clipRate)

func fixture(t *testing.T) assets.Blob {
	t.Helper()
	data, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	return assets.NewBlob(data)
}

// Prepare reads the Ogg headers and the stream length and keeps those three
// facts, which is the whole of what the contract must be able to report.
// Reader.Length matched the decoded frame count exactly on every clip it was
// measured against, so the duration is exact and not an estimate.
func TestPrepareReportsTheHeaderExactly(t *testing.T) {
	backend := newBackend(48000)

	prepared, done, err := backend.Prepare(nil, fixture(t))
	if err != nil {
		t.Fatalf("preparing the fixture clip: %v", err)
	}
	if !done {
		t.Fatal("Prepare was not done, and nosound parses the header inline")
	}
	if got := prepared.SampleRate(); got != clipRate {
		t.Fatalf("SampleRate = %d, want %d", got, clipRate)
	}
	if got := prepared.Channels(); got != clipChannels {
		t.Fatalf("Channels = %d, want %d", got, clipChannels)
	}
	if got := prepared.Duration(); got != clipDuration {
		t.Fatalf("Duration = %v, want %v", got, clipDuration)
	}
	if prepared.LoopRegion().Present() {
		t.Fatal("a Clip with no LOOPSTART tag reported a Loop Region")
	}
	if taken := backend.TakePrepared(); len(taken) != 0 {
		t.Fatalf("TakePrepared drained %d completions, and Prepare is always done", len(taken))
	}
}

// A duration of zero must never be silently accepted as a Clip's length. It
// makes the playhead and the duration fiction and silently removes
// ReasonFinished from every test that names that Clip, which is the one thing
// nosound exists to keep working.
func TestPrepareRefusesAStreamWithNoLength(t *testing.T) {
	backend := newBackend(48000)

	_, _, err := backend.Prepare(nil, assets.NewBlob(withoutGranule(t, fixture(t).Data())))

	var refused ErrNoStreamLength
	if !errors.As(err, &refused) {
		t.Fatalf("a stream reporting no length was prepared: err = %v", err)
	}
}

// Anything that is not Ogg Vorbis is a terminal failure, not a Clip. Every Clip
// failure is terminal - a bad path, a file that is not Ogg Vorbis, a corrupt
// stream - and there is no retry and no try-again.
func TestPrepareRefusesBytesThatAreNotOggVorbis(t *testing.T) {
	backend := newBackend(48000)

	_, _, err := backend.Prepare(nil, assets.NewBlobFromString("this is not an ogg file"))

	var refused ErrNotOggVorbis
	if !errors.As(err, &refused) {
		t.Fatalf("bytes that are not Ogg Vorbis were prepared: err = %v", err)
	}
	if refused.Unwrap() == nil {
		t.Fatal("the refusal carries nothing of what the decoder said")
	}
}

// nosound reports a working Device that plays nothing, not a missing one, so a
// game gating a "click to enable sound" prompt on Ready does not hang forever
// under it and a test does not special-case it. Latency is zero, which is true
// rather than a placeholder: nothing is buffered.
func TestTheDeviceIsReadyAndSilent(t *testing.T) {
	device := newBackend(44100).Device()

	if !device.Ready {
		t.Fatal("nosound's Device is not Ready")
	}
	if device.Name != string(Name) {
		t.Fatalf("the Device names %q, want %q", device.Name, Name)
	}
	if device.SampleRate != 44100 {
		t.Fatalf("the Device reports %d Hz, want the configured 44100", device.SampleRate)
	}
	if device.Channels != 2 {
		t.Fatalf("the Device reports %d channels, want stereo", device.Channels)
	}
	if device.Latency != 0 {
		t.Fatalf("the Device reports a latency of %v, and nothing is buffered", device.Latency)
	}
}

// Install mints a non-zero id per Clip, and no two Clips share one: zero means
// none, and an Adapter that repeated an id would have sound destroying a Clip
// another Voice is playing.
func TestInstallMintsANonZeroIdPerClip(t *testing.T) {
	backend := newBackend(48000)

	first, err := backend.Install(preparedClip{duration: 1, channels: 1, rate: 48000})
	if err != nil {
		t.Fatalf("installing: %v", err)
	}
	second, err := backend.Install(preparedClip{duration: 1, channels: 1, rate: 48000})
	if err != nil {
		t.Fatalf("installing: %v", err)
	}
	if first == 0 || second == 0 {
		t.Fatalf("Install minted %v and %v; zero means none", first, second)
	}
	if first == second {
		t.Fatalf("two Clips were installed under one id, %v", first)
	}
}

// Everything else nosound is handed is accepted and kept nowhere.
func TestEverythingElseIsAcceptedAndKeptNowhere(t *testing.T) {
	backend := newBackend(48000)

	backend.Voices(64)
	backend.Emit(&sound.Batch{
		Starts:   []sound.VoiceStart{{Slot: 0, Clip: 1}},
		Destroys: []sound.ClipID{1},
	})
}

// withoutGranule zeroes the granule position of a stream's last page, which is
// the page Length reads, and fixes that page's checksum so the stream is still
// well formed. It is how a test says "this file reports no length" without
// owning a truncated fixture whose other properties would drift.
func withoutGranule(t *testing.T, data []byte) []byte {
	t.Helper()
	out := bytes.Clone(data)
	last := -1
	for at := 0; at+27 <= len(out); {
		if !bytes.Equal(out[at:at+4], []byte("OggS")) {
			t.Fatalf("no Ogg capture pattern at offset %d", at)
		}
		segments := int(out[at+26])
		body := 0
		for _, size := range out[at+27 : at+27+segments] {
			body += int(size)
		}
		last = at
		at += 27 + segments + body
	}
	if last < 0 {
		t.Fatal("the fixture holds no Ogg pages")
	}
	clear(out[last+6 : last+14])
	clear(out[last+22 : last+26])
	binary.LittleEndian.PutUint32(out[last+22:last+26], oggCRC(out[last:]))
	return out
}

// oggCRC is Ogg's page checksum: a 32-bit CRC over the whole page with the
// checksum field zeroed, polynomial 0x04c11db7, no reflection and no final xor.
func oggCRC(page []byte) uint32 {
	var sum uint32
	for _, b := range page {
		sum = sum<<8 ^ oggCRCTable[byte(sum>>24)^b]
	}
	return sum
}

var oggCRCTable = func() (table [256]uint32) {
	for i := range table {
		value := uint32(i) << 24
		for range 8 {
			if value&0x80000000 != 0 {
				value = value<<1 ^ 0x04c11db7
			} else {
				value <<= 1
			}
		}
		table[i] = value
	}
	return table
}()
