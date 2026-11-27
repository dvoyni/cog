package headless_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/headless"
)

// The fixture clip's own facts, read off the file it was cut to. They are
// written down rather than computed so that a change to the file fails here.
const (
	clipFrames   = 48704
	clipRate     = 44100
	clipChannels = 2
)

// clipDuration is the fixture's length in seconds as the seam carries it,
// computed the way Prepare computes it.
const clipDuration = float32(clipFrames) / float32(clipRate)

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	return data
}

func TestNewBackendTakesZeroAsFortyEightKilohertz(t *testing.T) {
	if got := headless.NewBackend(0).Device().SampleRate; got != 48000 {
		t.Fatalf("NewBackend(0)'s device reports %d Hz, want 48000", got)
	}
}

func TestNewBackendPanicsOnANegativeRate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewBackend(-1) did not panic")
		}
	}()
	headless.NewBackend(-1)
}

// Prepare reads the Ogg headers and the stream length and keeps those facts,
// the whole of what the contract must be able to report. Reader.Length
// matched the decoded frame count exactly on every clip it was measured
// against, so the duration is exact and not an estimate.
func TestPrepareReportsTheHeaderExactly(t *testing.T) {
	backend := headless.NewBackend(48000)

	prepared, done, err := backend.Prepare(nil, readFixture(t))
	if err != nil {
		t.Fatalf("preparing the fixture clip: %v", err)
	}
	if !done {
		t.Fatal("Prepare was not done, and the headless backend parses the header inline")
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
		t.Fatal("a clip with no LOOPSTART tag reported a loop region")
	}
	if taken := backend.TakePrepared(); len(taken) != 0 {
		t.Fatalf("TakePrepared drained %d completions, and Prepare is always done", len(taken))
	}
}

// A duration of zero is never silently accepted as a clip's length: it would
// make the playhead and the duration fiction, and remove ReasonFinished from
// every test that names the clip.
func TestPrepareRefusesAStreamWithNoLength(t *testing.T) {
	_, _, err := headless.NewBackend(48000).Prepare(nil, clearLastGranule(t, readFixture(t)))

	var refused headless.ErrNoStreamLength
	if !errors.As(err, &refused) {
		t.Fatalf("a stream reporting no length was prepared: err = %v", err)
	}
}

// Anything that is not Ogg Vorbis is a terminal failure, not a clip.
func TestPrepareRefusesBytesThatAreNotOggVorbis(t *testing.T) {
	_, _, err := headless.NewBackend(48000).Prepare(nil, []byte("this is not an ogg file"))

	var refused headless.ErrNotOggVorbis
	if !errors.As(err, &refused) {
		t.Fatalf("bytes that are not Ogg Vorbis were prepared: err = %v", err)
	}
	if refused.Unwrap() == nil {
		t.Fatal("the refusal carries nothing of what the decoder said")
	}
}

// An identification header naming no sample rate gives no duration, so the
// clip is refused rather than prepared at a rate of zero.
func TestPrepareRefusesAStreamWithNoFormat(t *testing.T) {
	_, _, err := headless.NewBackend(48000).Prepare(nil, rewriteSampleRate(t, readFixture(t), 0))

	var refused headless.ErrNoStreamFormat
	if !errors.As(err, &refused) {
		t.Fatalf("a stream naming no sample rate was prepared: err = %v", err)
	}
	if refused.SampleRate != 0 || refused.Channels != clipChannels {
		t.Fatalf("the refusal names %d Hz and %d channels, want 0 Hz and %d channels",
			refused.SampleRate, refused.Channels, clipChannels)
	}
}

// The headless backend reports a working device that plays nothing, not a
// missing one, so a game gating anything on Ready does not wait forever under
// it and a test does not special-case it. Latency is zero, which is true
// rather than a placeholder: nothing is buffered.
func TestTheDeviceIsReadyAndSilent(t *testing.T) {
	backend := headless.NewBackend(44100)
	want := sfx.Device{Ready: true, Name: "headless", SampleRate: 44100, Channels: 2}

	if got := backend.Device(); got != want {
		t.Fatalf("the device is %+v, want %+v", got, want)
	}
	backend.Voices(64)
	backend.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: 1}}})
	if got := backend.Device(); got != want {
		t.Fatalf("after an Emit the device is %+v, want it constant at %+v", got, want)
	}
}

// Install mints a non-zero ID per clip, and no two clips share one: zero means
// none, and a backend that repeated an ID would have sfx destroying a clip
// another voice is playing.
func TestInstallMintsANonZeroIdPerClip(t *testing.T) {
	backend := headless.NewBackend(48000)
	prepared, _, err := backend.Prepare(nil, readFixture(t))
	if err != nil {
		t.Fatalf("preparing the fixture clip: %v", err)
	}

	first, err := backend.Install(prepared)
	if err != nil {
		t.Fatalf("installing: %v", err)
	}
	second, err := backend.Install(prepared)
	if err != nil {
		t.Fatalf("installing: %v", err)
	}
	if first == 0 || second == 0 {
		t.Fatalf("Install minted %v and %v; zero means none", first, second)
	}
	if first == second {
		t.Fatalf("two clips were installed under one ID, %v", first)
	}
}

// Everything else the headless backend is handed is accepted and kept
// nowhere.
func TestEverythingElseIsAcceptedAndKeptNowhere(t *testing.T) {
	backend := headless.NewBackend(48000)

	backend.Voices(64)
	backend.Emit(&sfx.FlushChanges{
		Starts:   []sfx.VoiceStart{{Slot: 0, Clip: 1}},
		Updates:  []sfx.VoiceUpdate{{Slot: 0}},
		Stops:    []sfx.VoiceSlot{0},
		Destroys: []sfx.ClipID{1},
	})
	if taken := backend.TakePrepared(); len(taken) != 0 {
		t.Fatalf("TakePrepared drained %d completions after an Emit", len(taken))
	}
}

// clearLastGranule zeroes the granule position of a stream's last page, the
// page Length reads, and fixes that page's checksum so the stream is still
// well formed: a file that reports no length without a truncated fixture whose
// other properties would drift.
func clearLastGranule(t *testing.T, data []byte) []byte {
	t.Helper()
	out := bytes.Clone(data)
	last := 0
	for at := 0; at < len(out); {
		head, body := measurePage(t, out[at:])
		last = at
		at += head + body
	}
	clear(out[last+6 : last+14])
	sealPage(t, out[last:])
	return out
}

// rewriteSampleRate rewrites the sample rate the identification header names,
// the first packet of the first page, and fixes that page's checksum.
func rewriteSampleRate(t *testing.T, data []byte, rate uint32) []byte {
	t.Helper()
	out := bytes.Clone(data)
	head, _ := measurePage(t, out)
	if out[head] != 1 || string(out[head+1:head+7]) != "vorbis" {
		t.Fatal("the fixture's first packet is not a Vorbis identification header")
	}
	// The type byte, the signature, a 32-bit version and a channel count come
	// before the rate.
	binary.LittleEndian.PutUint32(out[head+12:head+16], rate)
	sealPage(t, out)
	return out
}

// measurePage answers the length of the header, its segment table included, and
// of the body of the Ogg page that page begins with.
func measurePage(t *testing.T, page []byte) (head, body int) {
	t.Helper()
	if len(page) < 27 || string(page[:4]) != "OggS" {
		t.Fatal("no Ogg capture pattern where a page should begin")
	}
	head = 27 + int(page[26])
	for _, size := range page[27:head] {
		body += int(size)
	}
	return head, body
}

// sealPage recomputes the checksum of the Ogg page that page begins with.
func sealPage(t *testing.T, page []byte) {
	t.Helper()
	head, body := measurePage(t, page)
	clear(page[22:26])
	binary.LittleEndian.PutUint32(page[22:26], computeOggCRC(page[:head+body]))
}

// computeOggCRC answers Ogg's page checksum: a 32-bit CRC over the whole page
// with the checksum field zeroed, polynomial 0x04c11db7, no reflection and no
// final xor.
func computeOggCRC(page []byte) uint32 {
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
