//go:build !js

package otosound

import (
	"errors"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/headless"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// makeTestBackend answers the backend of an otosound spawned with config over
// a fake device, in a kernel with no flush scheduled, so the test alone calls
// it, as sfx's flush would.
func makeTestBackend(t testing.TB, config Config) backend {
	t.Helper()
	plugin := NewPlugin()
	spawnWithConfig(t, plugin, &config)
	return backend{plugin: plugin}
}

// waitPrepared drains TakePrepared until count prepares have finished, as
// sfx's flush drains it a flush at a time, and answers them.
func waitPrepared(t testing.TB, b backend, count int) []sfx.Prepared {
	t.Helper()
	var completions []sfx.Prepared
	deadline := time.Now().Add(sfxtest.Timeout)
	for len(completions) < count {
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d prepares finished in %v", len(completions), count, sfxtest.Timeout)
		}
		completions = append(completions, b.TakePrepared()...)
		time.Sleep(time.Millisecond)
	}
	if len(completions) != count {
		t.Fatalf("TakePrepared drained %d completions, want %d", len(completions), count)
	}
	return completions
}

// Prepare answers not done and decodes on a goroutine of its own, so neither
// the flush nor the device thread carries a decode. The completion lands
// under the token sfx handed it, which is how it finds the entry it belongs
// to, and is taken once.
func TestPrepareDecodesOffTheFlushAndTheCompletionArrivesUnderItsToken(t *testing.T) {
	b := makeTestBackend(t, Config{})
	token := struct{ name string }{"pianoroll"}

	prepared, done, err := b.Prepare(token, readFixture(t))
	if prepared != nil || done || err != nil {
		t.Fatalf("Prepare answered %v, %v and %v, want not done, since otosound decodes on a goroutine", prepared, done, err)
	}

	completion := waitPrepared(t, b, 1)[0]
	if completion.Token != any(token) {
		t.Fatalf("the completion carries token %v, want the one Prepare was handed", completion.Token)
	}
	if completion.Err != nil {
		t.Fatalf("decoding the fixture clip: %v", completion.Err)
	}
	clip := completion.Clip
	if clip.Duration() != clipDuration || clip.Channels() != clipChannels || clip.SampleRate() != clipRate {
		t.Fatalf("the clip reports %v s, %d channels and %d Hz, want the file's %v, %d and %d",
			clip.Duration(), clip.Channels(), clip.SampleRate(), clipDuration, clipChannels, clipRate)
	}
	if clip.LoopRegion().Present() || clip.LoopRegionErr() != nil {
		t.Fatal("a clip with no loop tags reported a loop region")
	}
	if again := b.TakePrepared(); len(again) != 0 {
		t.Fatalf("TakePrepared answered %d completions again, and it clears what it answers", len(again))
	}
}

// Both backends read one clip's facts identically: the fixture prepared here
// and under the headless backend answers the same duration, channels, rate
// and loop region, a malformed region's error included.
func TestOtosoundAndTheHeadlessBackendReportOneClipIdentically(t *testing.T) {
	for name, encoded := range map[string][]byte{
		"untagged":  readFixture(t),
		"tagged":    retagClip(t, readFixture(t), "LOOPSTART=11025", "LOOPEND=33075"),
		"malformed": retagClip(t, readFixture(t), "LOOPSTART=11025", "LOOPEND=48705"),
	} {
		t.Run(name, func(t *testing.T) {
			want, _, err := headless.NewBackend(0).Prepare(nil, encoded)
			if err != nil {
				t.Fatalf("preparing the clip under the headless backend: %v", err)
			}
			got, err := prepareClip(encoded, defaultDecodedClipLimit)
			if err != nil {
				t.Fatalf("preparing the clip under otosound: %v", err)
			}
			if got.Duration() != want.Duration() || got.Channels() != want.Channels() || got.SampleRate() != want.SampleRate() ||
				got.LoopRegion() != want.LoopRegion() || !errors.Is(got.LoopRegionErr(), want.LoopRegionErr()) {
				t.Fatalf("otosound reports %v, %d, %d, %+v and %v, and the headless backend %v, %d, %d, %+v and %v",
					got.Duration(), got.Channels(), got.SampleRate(), got.LoopRegion(), got.LoopRegionErr(),
					want.Duration(), want.Channels(), want.SampleRate(), want.LoopRegion(), want.LoopRegionErr())
			}
		})
	}
}

// Bytes that are not Ogg Vorbis fail the prepare with ErrNotOggVorbis, a
// failure that is terminal, and hand sfx no clip: a typed nil would be a
// non-nil prepared clip holding nothing, and sfx would install it.
func TestPrepareRefusesBytesThatAreNotOggVorbis(t *testing.T) {
	b := makeTestBackend(t, Config{})
	b.Prepare(nil, []byte("this is not an ogg stream"))

	completion := waitPrepared(t, b, 1)[0]
	var refused ErrNotOggVorbis
	if !errors.As(completion.Err, &refused) {
		t.Fatalf("bytes that are not Ogg Vorbis answered %v, want ErrNotOggVorbis", completion.Err)
	}
	if completion.Clip != nil {
		t.Fatalf("a failed prepare handed back %#v, which sfx would install", completion.Clip)
	}
}

// An identification header naming no rate, no channels, or more channels
// than the 2×2 gain matrix addresses fails the prepare with
// ErrNoStreamFormat, refused rather than truncated.
func TestPrepareRefusesAStreamWithNoFormat(t *testing.T) {
	for name, test := range map[string]struct {
		channels byte
		rate     uint32
	}{
		"no rate":        {clipChannels, 0},
		"no channels":    {0, clipRate},
		"three channels": {3, clipRate},
	} {
		t.Run(name, func(t *testing.T) {
			b := makeTestBackend(t, Config{})
			b.Prepare(nil, rewriteFormat(t, readFixture(t), test.channels, test.rate))

			completion := waitPrepared(t, b, 1)[0]
			want := ErrNoStreamFormat{SampleRate: int(test.rate), Channels: int(test.channels)}
			var refused ErrNoStreamFormat
			if !errors.As(completion.Err, &refused) || refused != want {
				t.Fatalf("the prepare answered %v, want %v", completion.Err, want)
			}
			if completion.Clip != nil {
				t.Fatalf("a failed prepare handed back %#v, which sfx would install", completion.Clip)
			}
		})
	}
}

// A stream whose length reads zero fails the prepare with ErrNoStreamLength,
// as it does under the headless backend, until streaming is built to count
// its frames.
func TestPrepareRefusesAStreamWithNoLength(t *testing.T) {
	b := makeTestBackend(t, Config{})
	b.Prepare(nil, clearLastGranule(t, readFixture(t)))

	completion := waitPrepared(t, b, 1)[0]
	if !errors.Is(completion.Err, ErrNoStreamLength{}) {
		t.Fatalf("a stream whose length reads zero answered %v, want ErrNoStreamLength", completion.Err)
	}
}

// Install mints the ID on sfx's flush, where the destroy that pairs with it
// is statable, and an ID is never zero and never repeats; each is kept in
// the clip table under it.
func TestInstallMintsANonZeroIdThatNeverRepeats(t *testing.T) {
	b := makeTestBackend(t, Config{})
	clip := &preparedClip{samples: []float32{0}, frames: 1, channels: 1, sampleRate: clipRate}

	var minted []sfx.ClipID
	for range 3 {
		id, err := b.Install(clip)
		if err != nil {
			t.Fatalf("installing a prepared clip: %v", err)
		}
		minted = append(minted, id)
	}
	if slices.Contains(minted, 0) || len(slices.Compact(slices.Sorted(slices.Values(minted)))) != len(minted) {
		t.Fatalf("Install minted %v", minted)
	}
	for _, id := range minted {
		if b.plugin.clips[id] != clip {
			t.Fatalf("the clip table holds %v under %d, want the clip installed", b.plugin.clips[id], id)
		}
	}
}

// Install refuses a prepared clip otosound did not make, a nil one among
// them, and mints nothing for it.
func TestInstallRefusesAPreparedClipOtosoundDidNotMake(t *testing.T) {
	b := makeTestBackend(t, Config{})
	for name, clip := range map[string]sfx.PreparedClip{
		"another backend's": otherClip{},
		"a nil clip":        (*preparedClip)(nil),
		"none":              nil,
	} {
		t.Run(name, func(t *testing.T) {
			if id, err := b.Install(clip); !errors.Is(err, errNotOurClip) || id != 0 {
				t.Fatalf("Install answered %d and %v, want errNotOurClip", id, err)
			}
		})
	}
	if len(b.plugin.clips) != 0 {
		t.Fatalf("the clip table holds %d clips, want none", len(b.plugin.clips))
	}
}

// Prepares run on goroutines of their own while the flush drains
// TakePrepared: every completion arrives exactly once, under its own token.
// Run under -race, it fails with finishedPreparesMutex taken out of TakePrepared.
func TestTakePreparedDrainsEveryCompletionWhileOthersFinish(t *testing.T) {
	b := makeTestBackend(t, Config{})
	const prepareCount = 16
	encoded := readFixture(t)
	for token := range prepareCount {
		b.Prepare(token, encoded)
	}

	completions := waitPrepared(t, b, prepareCount)
	var tokens []int
	for _, completion := range completions {
		if completion.Err != nil {
			t.Fatalf("prepare %v failed: %v", completion.Token, completion.Err)
		}
		tokens = append(tokens, completion.Token.(int))
	}
	slices.Sort(tokens)
	for token := range prepareCount {
		if tokens[token] != token {
			t.Fatalf("the completions carry tokens %v, want each of 0 to %d once", tokens, prepareCount-1)
		}
	}
}

// Destroy waits for the prepares in flight, so a destroyed kernel leaves no
// goroutine of otosound's decoding.
func TestDestroyWaitsForThePreparesInFlight(t *testing.T) {
	const prepareCount = 4
	var b backend
	t.Run("the kernel, destroyed as it ends", func(t *testing.T) {
		b = makeTestBackend(t, Config{})
		for token := range prepareCount {
			b.Prepare(token, readFixture(t))
		}
	})
	if completions := b.TakePrepared(); len(completions) != prepareCount {
		t.Fatalf("%d of %d prepares had finished when Destroy returned", len(completions), prepareCount)
	}
}

// Nothing decodes on the device thread: the files the device thread runs
// import no package that could, only those its own work needs.
func TestTheDeviceThreadsFilesImportNothingThatCouldDecode(t *testing.T) {
	allowed := []string{"encoding/binary", "math", "sync/atomic", "time", "github.com/dvoyni/cog/plugins/sfx"}
	for _, file := range []string{"mixer.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("reading an import of %s: %v", file, err)
			}
			if !slices.Contains(allowed, path) {
				t.Errorf("%s, which the device thread runs, imports %q, and it may import only %v", file, path, allowed)
			}
		}
	}
}

// A game playing a clip under the limit hears of it as under any backend:
// its voice waits on the prepare, binds to the duration the headless backend
// reports for the same bytes, and ends when the clip says it would.
func TestAGamePlayingAClipUnderTheLimitBindsItsVoiceToItsDuration(t *testing.T) {
	const path = "pianoroll.ogg"
	h, _ := spawnOverFake(t, &fakeDevice{})
	h.Mount(path, readFixture(t))
	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) { voice = queue.Play(path, 0, sfx.VoiceOptions{}) })

	duration := clipDuration
	want := time.Duration(float64(duration) * float64(time.Second))
	deadline := time.Now().Add(sfxtest.Timeout)
	for {
		h.Step()
		var entry sfx.VoiceEntry
		h.Read(func(voices sfx.LiveVoices, _ sfx.ClipTable, _ sfx.BusVolumes, _ sfx.Listener, _ sfx.DeviceView) {
			entry, _ = voices.Info(voice)
		})
		if entry.Duration != 0 {
			if entry.Duration != want {
				t.Fatalf("the voice is bound to %v, want the clip's %v", entry.Duration, want)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the voice waited on its clip for %v", sfxtest.Timeout)
		}
		time.Sleep(time.Millisecond)
	}

	// The voice started from its head at the flush that bound it, so it ends
	// a duration's worth of steps later, rounded up; a step or two more adds
	// no ending.
	for range int(want/sfxtest.StepInterval) + 3 {
		h.Step()
	}
	if endings := h.Endings(); !slices.Equal(endings, []sfx.VoiceEndedEvent{{Voice: voice, Reason: sfx.ReasonFinished}}) {
		t.Fatalf("the endings are %v, want the voice finished", endings)
	}
}

// otherClip is a prepared clip otosound did not make.
type otherClip struct{}

func (otherClip) Duration() float32                   { return 1 }
func (otherClip) Channels() int                       { return 1 }
func (otherClip) SampleRate() int                     { return clipRate }
func (otherClip) LoopRegion() m.Maybe[sfx.LoopRegion] { return m.Maybe[sfx.LoopRegion]{} }
func (otherClip) LoopRegionErr() error                { return nil }
