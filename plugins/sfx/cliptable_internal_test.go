package sfx

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
)

// A completion whose entry has left the table, or moved to another epoch, as
// a release recorded against the prepare in flight leaves it, is dropped with
// nothing called: no ClipID is minted only to be destroyed. The release is
// stood in for by taking the entry out of the table, which is all a release
// does to it.
func TestACompletionForAClipReleasedBeforeItArrivedMintsNoClipID(t *testing.T) {
	const path = "music/pianoroll.ogg"
	k := spawnBareKernel(t)
	files := fstest.MapFS{path: {Data: []byte("a clip")}}
	backend := &deferringBackend{}
	clips := makeClipTable()

	clips.load(k, files, backend, path)
	delete(clips.state.entries, path)
	backend.finish()
	clips.drain(k, backend)
	if backend.installCount != 0 {
		t.Fatalf("a completion for a clip no longer held minted %d IDs", backend.installCount)
	}

	clips.load(k, files, backend, path)
	delete(clips.state.entries, path)
	backend.finish()
	clips.load(k, files, backend, path)
	clips.drain(k, backend)
	if entry := clips.state.entries[path]; backend.installCount != 0 || entry.state != clipLoading {
		t.Fatalf("a completion of an earlier epoch left the entry named again %+v, with %d IDs minted", entry, backend.installCount)
	}

	backend.finish()
	clips.drain(k, backend)
	if entry := clips.state.entries[path]; backend.installCount != 1 || entry.state != clipReady || entry.id != 1 {
		t.Fatalf("the completion of the entry's own epoch left it %+v, with %d IDs minted, want it ready under the one ID", entry, backend.installCount)
	}
}

// spawnBareKernel answers a kernel of no plugin, for a test of a part of sfx
// that logs through one, destroyed when the test ends.
func spawnBareKernel(t *testing.T) *kernel.Kernel {
	t.Helper()
	supervisor := kernel.NewSupervisor(nil)
	t.Cleanup(func() {
		supervisor.Quit()
		<-supervisor.Done()
	})
	id, err := supervisor.Spawn(nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	return supervisor.GetKernel(id)
}
