package assets_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
)

// bake is the fake family's bake parameters: the part of a descriptor that is
// not the source, and what makes one source several assets.
type bake struct {
	Variant int
}

// face is what the fake loader decodes to. It carries what the load saw, so a
// test can tell which bytes reached Load and whether the value came from Load
// or from Default.
type face struct {
	body    string
	variant int
	missing bool
}

// device stands in for the plugin-defined pass-through: whatever the loader
// needs that only a handler holds. The cache never inspects it, so the test
// asserts it arrives unchanged.
type device struct {
	name string
}

// loader is the fake Loader the Library's own tests run against: it counts
// every call and records what each one was given.
type loader struct {
	loads    int
	defaults int
	freed    []face
	devices  []*device

	// onFree, when set, runs inside Free. It is how the re-entrancy tests reach
	// back into the cache while a walk is in progress.
	onFree func(freed face)
}

func (l *loader) Load(_ kernel.Kernel, data assets.Blob, params bake, _ fs.FS, user *device) face {
	l.loads++
	l.devices = append(l.devices, user)
	return face{body: data.String(), variant: params.Variant}
}

func (l *loader) Default(d assets.Descr[bake], user *device) face {
	l.defaults++
	l.devices = append(l.devices, user)
	return face{body: d.Name, variant: d.Params.Variant, missing: true}
}

func (l *loader) Free(value face, user *device) {
	l.freed = append(l.freed, value)
	l.devices = append(l.devices, user)
	if l.onFree != nil {
		l.onFree(value)
	}
}

// reportingKernel hands back a kernel whose handler collects what the Library
// reports. The engine is never run: report-once needs it only for the table it
// keys and the handler it calls, both of which exist from New.
func reportingKernel() (kernel.Kernel, *[]error) {
	var reported []error
	engine := kernel.New(nil).Handler(func(err error) bool {
		reported = append(reported, err)
		return false
	})
	return engine.Executioner().Kernel, &reported
}

// The Library reads and the loader decodes: a path-named descriptor arrives at
// Load as the file's bytes, with the bake parameters and the pass-through
// beside them.
func TestGetReadsThePathAndHandsTheBytesToLoad(t *testing.T) {
	k, reported := reportingKernel()
	fsys := fstest.MapFS{"shaders/app.wgsl": &fstest.MapFile{Data: []byte("fn main() {}")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	gpu := &device{name: "gpu"}

	got := cache.Get(k, assets.Descr[bake]{Name: "shaders/app.wgsl", Params: bake{Variant: 2}}, fsys, gpu)

	if got != (face{body: "fn main() {}", variant: 2}) {
		t.Fatalf("Get returned %+v, want the decoded file", got)
	}
	if l.loads != 1 || l.defaults != 0 {
		t.Fatalf("%d loads and %d defaults, want exactly one load", l.loads, l.defaults)
	}
	if len(l.devices) != 1 || l.devices[0] != gpu {
		t.Fatalf("the loader saw %v, want the pass-through unchanged", l.devices)
	}
	if len(*reported) != 0 {
		t.Fatalf("reported %v, want silence on a read that worked", *reported)
	}
}

// A descriptor with no Name skips the read: the bytes are already in hand, so
// no filesystem is consulted and none need be supplied.
func TestABlobNamedAssetSkipsTheRead(t *testing.T) {
	k, _ := reportingKernel()
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	got := cache.Get(k, assets.Descr[bake]{Blob: assets.NewBlobFromString("// inline")}, nil, nil)

	if got.body != "// inline" {
		t.Fatalf("Get returned %+v, want the caller's own bytes", got)
	}
	if l.loads != 1 {
		t.Fatalf("%d loads, want one", l.loads)
	}
}

// You named it, you own the name. A Blob beside a Name is a payload: it is the
// key that ignores it, not the load, so two descriptors differing only in their
// Blob are one entry and the first one's bytes are what both get.
func TestNameWinsAndTheBlobBesideItIsPayload(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{"models/crate.glb": &fstest.MapFile{Data: []byte("from the path")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	first := cache.Get(k, assets.Descr[bake]{Name: "models/crate.glb", Blob: assets.NewBlobFromString("mine")}, fsys, nil)
	second := cache.Get(k, assets.Descr[bake]{Name: "models/crate.glb", Blob: assets.NewBlobFromString("yours")}, fsys, nil)

	if first.body != "mine" {
		t.Fatalf("Load saw %q, want the payload supplied beside the name", first.body)
	}
	if second != first {
		t.Fatalf("a second Blob under one Name produced %+v, want the first arrival", second)
	}
	if l.loads != 1 {
		t.Fatalf("%d loads, want the Name alone to be the key", l.loads)
	}
}

// A payload is read from nobody. The container an embedded asset is named by is
// never opened to reach it, which is the whole reason a Blob may ride beside a
// Name: the alternative is the Library reading a GLB per image and the loader
// re-parsing it to find image N.
func TestAPayloadBesideANameIsNeverReadFromStorage(t *testing.T) {
	k, reported := reportingKernel()
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	// The name does not exist and is never opened, so nothing is reported and
	// the loader still sees the caller's own bytes.
	got := cache.Get(k, assets.Descr[bake]{
		Name: "models/crate.glb", Blob: assets.NewBlobFromString("image 0"),
	}, fstest.MapFS{}, nil)

	if got.body != "image 0" {
		t.Fatalf("Load saw %q, want the bytes the caller already held", got.body)
	}
	if len(*reported) != 0 {
		t.Fatalf("reported %v, want no read and therefore no read failure", *reported)
	}
}

// A path with no payload is still read, and a read that fails is still the
// Library's own to report - the payload rule narrows which descriptors reach
// the filesystem and changes nothing about what happens when one does.
func TestANameWithNoPayloadIsStillRead(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{"models/crate.glb": &fstest.MapFile{Data: []byte("from the path")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	got := cache.Get(k, assets.Descr[bake]{Name: "models/crate.glb"}, fsys, nil)
	if got.body != "from the path" {
		t.Fatalf("Load saw %q, want the file the Name pointed at", got.body)
	}
}

// For a blob-named asset the bytes are the identity, so the same run wrapped
// twice is one entry and a fresh copy of those bytes is another.
func TestABlobNamedAssetKeysOnTheRunOfBytes(t *testing.T) {
	k, _ := reportingKernel()
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	const source = "// custom"

	cache.Get(k, assets.Descr[bake]{Blob: assets.NewBlobFromString(source)}, nil, nil)
	cache.Get(k, assets.Descr[bake]{Blob: assets.NewBlobFromString(source)}, nil, nil)
	if l.loads != 1 {
		t.Fatalf("%d loads, want one string literal to be one identity", l.loads)
	}

	for range 5 {
		cache.Get(k, assets.Descr[bake]{Blob: assets.NewBlob([]byte(source))}, nil, nil)
	}
	if l.loads != 6 {
		t.Fatalf("%d loads, want five []byte conversions to be five identities on top of the first", l.loads)
	}
}

// Blob{} is one empty, so every way of spelling no bytes reaches the same
// entry - which is also what makes a path-named descriptor legal.
func TestEveryEmptyBlobIsOneEntry(t *testing.T) {
	k, _ := reportingKernel()
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	cache.Get(k, assets.Descr[bake]{}, nil, nil)
	cache.Get(k, assets.Descr[bake]{Blob: assets.NewBlob(nil)}, nil, nil)
	cache.Get(k, assets.Descr[bake]{Blob: assets.NewBlobFromString("")}, nil, nil)

	if l.loads != 1 {
		t.Fatalf("%d loads, want every empty Blob to be one entry", l.loads)
	}
}

// The bake parameters are part of the key, which is what makes one source
// several assets.
func TestDescriptorsDifferingOnlyInParamsAreTwoEntries(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{"fonts/body.ttf": &fstest.MapFile{Data: []byte("glyphs")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	small := cache.Get(k, assets.Descr[bake]{Name: "fonts/body.ttf", Params: bake{Variant: 12}}, fsys, nil)
	large := cache.Get(k, assets.Descr[bake]{Name: "fonts/body.ttf", Params: bake{Variant: 24}}, fsys, nil)

	if l.loads != 2 {
		t.Fatalf("%d loads, want one per size", l.loads)
	}
	if small.variant == large.variant {
		t.Fatalf("both sizes decoded to variant %d", small.variant)
	}
}

// A hit is a hit: the second Get of one descriptor returns the cached value and
// does not load again.
func TestASecondGetIsCached(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{"textures/rust.png": &fstest.MapFile{Data: []byte("pixels")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	d := assets.Descr[bake]{Name: "textures/rust.png"}

	first := cache.Get(k, d, fsys, nil)
	second := cache.Get(k, d, fsys, nil)

	if first != second {
		t.Fatalf("two Gets returned %+v and %+v", first, second)
	}
	if l.loads != 1 {
		t.Fatalf("%d loads, want one", l.loads)
	}
}

// The Library owns the read failure. It reports it once, names the asset by the
// type it would have produced, and wraps storage's error so a caller who wants
// to can still ask whether the file was absent.
func TestAMissingFileIsReportedOnceByTheLibrary(t *testing.T) {
	k, reported := reportingKernel()
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	d := assets.Descr[bake]{Name: "sprites/hero.png"}

	cache.Get(k, d, fstest.MapFS{}, nil)

	if len(*reported) != 1 {
		t.Fatalf("reported %v, want exactly one error", *reported)
	}
	got := (*reported)[0].Error()
	const want = `asset assets_test.face not found by path "sprites/hero.png"`
	if len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("reported %q, want it to start %q", got, want)
	}
	if !errors.Is((*reported)[0], fs.ErrNotExist) {
		t.Fatalf("reported %v, want storage's own error wrapped untouched", (*reported)[0])
	}
}

// Failure is terminal: the value Default supplied is cached like any other, so
// the load does not run again and the report does not repeat.
func TestAFailedLoadIsTerminalAndSilentAfterTheFirstReport(t *testing.T) {
	k, reported := reportingKernel()
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	d := assets.Descr[bake]{Name: "sprites/hero.png", Params: bake{Variant: 3}}

	first := cache.Get(k, d, fstest.MapFS{}, nil)
	second := cache.Get(k, d, fstest.MapFS{}, nil)

	if !first.missing || first.variant != 3 {
		t.Fatalf("Get returned %+v, want Default's value shaped by the descriptor", first)
	}
	if second != first {
		t.Fatalf("the second Get returned %+v, want the cached failure", second)
	}
	if l.loads != 0 {
		t.Fatalf("%d loads, want none: the read never produced bytes", l.loads)
	}
	if l.defaults != 1 {
		t.Fatalf("%d Default calls, want one", l.defaults)
	}
	if len(*reported) != 1 {
		t.Fatalf("reported %v, want one report across two Gets", *reported)
	}
}

// Freeing is the retry lever, and the paired forget is what lets the condition
// speak again: a freed failure reloads and reports afresh.
func TestFreeingAFailedEntryReloadsAndReportsAfresh(t *testing.T) {
	k, reported := reportingKernel()
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	d := assets.Descr[bake]{Name: "sprites/hero.png"}

	cache.Get(k, d, fstest.MapFS{}, nil)
	cache.Free(k, d, nil)
	cache.Get(k, d, fstest.MapFS{}, nil)

	if l.defaults != 2 {
		t.Fatalf("%d Default calls, want the freed entry to load again", l.defaults)
	}
	if len(*reported) != 2 {
		t.Fatalf("reported %v, want the freed key to speak again", *reported)
	}
}

// A Free reaches the loader, because T holds backend handles that dropping the
// entry alone would leak - and it reaches it once per entry.
func TestFreeReleasesTheValueThroughTheLoader(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{"textures/rust.png": &fstest.MapFile{Data: []byte("pixels")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	gpu := &device{name: "gpu"}
	d := assets.Descr[bake]{Name: "textures/rust.png"}

	loaded := cache.Get(k, d, fsys, gpu)
	cache.Free(k, d, gpu)
	cache.Free(k, d, gpu)

	if len(l.freed) != 1 || l.freed[0] != loaded {
		t.Fatalf("freed %v, want the loaded value exactly once", l.freed)
	}
	if reloaded := cache.Get(k, d, fsys, gpu); l.loads != 2 || reloaded != loaded {
		t.Fatalf("after a Free the entry is %+v after %d loads, want a reload", reloaded, l.loads)
	}
}

// A payload Blob is not part of the key, so a Free naming the path alone
// retires the entry a Get supplied bytes beside.
func TestFreeKeysTheSameWayGetDoes(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{"models/crate.glb": &fstest.MapFile{Data: []byte("mesh")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	cache.Get(k, assets.Descr[bake]{Name: "models/crate.glb", Blob: assets.NewBlobFromString("payload")}, fsys, nil)
	cache.Free(k, assets.Descr[bake]{Name: "models/crate.glb"}, nil)

	if len(l.freed) != 1 {
		t.Fatalf("freed %v, want the entry the payload was supplied beside", l.freed)
	}
}

// FreeAll retires whatever is in the table at the call, frees each entry
// exactly once, and lets the whole family's reports speak again.
func TestFreeAllRetiresEveryEntryAndForgetsTheFamily(t *testing.T) {
	k, reported := reportingKernel()
	fsys := fstest.MapFS{"textures/rust.png": &fstest.MapFile{Data: []byte("pixels")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	cache.Get(k, assets.Descr[bake]{Name: "textures/rust.png"}, fsys, nil)
	cache.Get(k, assets.Descr[bake]{Name: "textures/rust.png", Params: bake{Variant: 1}}, fsys, nil)
	cache.Get(k, assets.Descr[bake]{Name: "textures/gone.png"}, fsys, nil)
	if len(*reported) != 1 {
		t.Fatalf("reported %v, want the one missing file", *reported)
	}

	cache.FreeAll(k, nil)

	if len(l.freed) != 3 {
		t.Fatalf("freed %d entries, want all three", len(l.freed))
	}
	cache.Get(k, assets.Descr[bake]{Name: "textures/gone.png"}, fsys, nil)
	if len(*reported) != 2 {
		t.Fatalf("reported %v, want the freed family to speak again", *reported)
	}
	if l.loads != 2 {
		t.Fatalf("%d loads, want the two readable entries loaded once each", l.loads)
	}
}

// FreeWhere is for the release decision the caller cannot spell as a key: the
// value holds the answer. It frees what it matched and leaves the rest resident.
func TestFreeWhereEvictsWhatTheValueSays(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{
		"fonts/body.ttf":    &fstest.MapFile{Data: []byte("body")},
		"fonts/display.ttf": &fstest.MapFile{Data: []byte("display")},
	}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	small := cache.Get(k, assets.Descr[bake]{Name: "fonts/body.ttf", Params: bake{Variant: 12}}, fsys, nil)
	large := cache.Get(k, assets.Descr[bake]{Name: "fonts/body.ttf", Params: bake{Variant: 24}}, fsys, nil)
	other := cache.Get(k, assets.Descr[bake]{Name: "fonts/display.ttf"}, fsys, nil)

	cache.FreeWhere(k, nil, func(_ assets.Descr[bake], value face) bool { return value.body == "body" })

	if len(l.freed) != 2 {
		t.Fatalf("freed %v, want both faces baked from the one file", l.freed)
	}
	if freedSet := map[face]bool{l.freed[0]: true, l.freed[1]: true}; !freedSet[small] || !freedSet[large] {
		t.Fatalf("freed %v, want %+v and %+v", l.freed, small, large)
	}
	if again := cache.Get(k, assets.Descr[bake]{Name: "fonts/display.ttf"}, fsys, nil); again != other || l.loads != 3 {
		t.Fatalf("the unmatched entry reloaded after %d loads, want it left resident", l.loads)
	}
}

// FreeWhere is shown the descriptor the table keys by, so a match on the path
// reaches an entry whose Get carried a payload beside the name.
func TestFreeWhereSeesTheKeyDescriptor(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{"models/crate.glb": &fstest.MapFile{Data: []byte("mesh")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)

	cache.Get(k, assets.Descr[bake]{Name: "models/crate.glb", Blob: assets.NewBlobFromString("payload")}, fsys, nil)

	var seen []assets.Descr[bake]
	cache.FreeWhere(k, nil, func(d assets.Descr[bake], _ face) bool {
		seen = append(seen, d)
		return d.Name == "models/crate.glb"
	})

	if len(seen) != 1 || seen[0] != (assets.Descr[bake]{Name: "models/crate.glb"}) {
		t.Fatalf("match saw %+v, want the key rather than the payload", seen)
	}
	if len(l.freed) != 1 {
		t.Fatalf("freed %v, want the matched entry", l.freed)
	}
}

// The ordering rule, asserted by the loader that would break it: Free runs
// after the entry has left the table, so a loader re-entering its own cache
// inside Free reloads rather than seeing what is being retired - and the walk
// it re-enters is not corrupted by what it adds.
func TestFreeAllRemovesBeforeItFreesAndSurvivesReentry(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{
		"textures/a.png": &fstest.MapFile{Data: []byte("a")},
		"textures/b.png": &fstest.MapFile{Data: []byte("b")},
		"textures/c.png": &fstest.MapFile{Data: []byte("c")},
	}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	paths := []string{"textures/a.png", "textures/b.png", "textures/c.png"}
	for _, path := range paths {
		cache.Get(k, assets.Descr[bake]{Name: path}, fsys, nil)
	}

	// The re-entrant Get asks for the very entry being freed. If Free ran
	// before the removal it would hit the dying entry; if the removal happened
	// one key at a time under a live range, what it re-adds could be visited or
	// silently truncated.
	var reentered []face
	l.onFree = func(freed face) {
		reentered = append(reentered, cache.Get(k, assets.Descr[bake]{Name: "textures/" + freed.body + ".png"}, fsys, nil))
	}

	cache.FreeAll(k, nil)

	if len(l.freed) != 3 {
		t.Fatalf("freed %v, want each of the three exactly once", l.freed)
	}
	if l.loads != 6 {
		t.Fatalf("%d loads, want the three originals plus three reloads", l.loads)
	}
	if len(reentered) != 3 {
		t.Fatalf("the loader re-entered %d times, want three", len(reentered))
	}

	// What the re-entrant Gets put back is in the table at the call, and
	// FreeAll walked what was there when it started, so all three survive.
	l.onFree = nil
	before := l.loads
	for _, path := range paths {
		cache.Get(k, assets.Descr[bake]{Name: path}, fsys, nil)
	}
	if l.loads != before {
		t.Fatalf("%d loads after the walk, want the entries added during it to have survived", l.loads-before)
	}
}

// FreeWhere collects, removes and then frees for the same reason FreeAll does,
// and an entry its loader adds during the walk is not matched against by it.
func TestFreeWhereRemovesBeforeItFreesAndSurvivesReentry(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{
		"textures/a.png": &fstest.MapFile{Data: []byte("a")},
		"textures/b.png": &fstest.MapFile{Data: []byte("b")},
	}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	cache.Get(k, assets.Descr[bake]{Name: "textures/a.png"}, fsys, nil)
	cache.Get(k, assets.Descr[bake]{Name: "textures/b.png"}, fsys, nil)

	matched := 0
	l.onFree = func(freed face) {
		cache.Get(k, assets.Descr[bake]{Name: "textures/" + freed.body + ".png"}, fsys, nil)
	}

	cache.FreeWhere(k, nil, func(_ assets.Descr[bake], _ face) bool {
		matched++
		return true
	})

	if matched != 2 {
		t.Fatalf("match ran %d times, want once per entry in the table at the call", matched)
	}
	if len(l.freed) != 2 {
		t.Fatalf("freed %v, want both entries exactly once", l.freed)
	}
	if l.loads != 4 {
		t.Fatalf("%d loads, want the two originals plus the two the loader re-entered for", l.loads)
	}
}

// The pass-through reaches every verb that calls the loader, unchanged, and the
// cache never inspects it.
func TestTheUserPassesThroughUntouched(t *testing.T) {
	k, _ := reportingKernel()
	fsys := fstest.MapFS{"textures/rust.png": &fstest.MapFile{Data: []byte("pixels")}}
	l := &loader{}
	cache := assets.New[bake, *device, face](l)
	gpu := &device{name: "gpu"}
	d := assets.Descr[bake]{Name: "textures/rust.png"}

	cache.Get(k, d, fsys, gpu)
	cache.Free(k, d, gpu)
	cache.Get(k, d, fsys, gpu)
	cache.FreeAll(k, gpu)
	cache.Get(k, assets.Descr[bake]{Name: "textures/gone.png"}, fsys, gpu)
	cache.FreeWhere(k, gpu, func(assets.Descr[bake], face) bool { return true })

	if len(l.devices) == 0 {
		t.Fatal("the loader was never called")
	}
	for i, seen := range l.devices {
		if seen != gpu {
			t.Fatalf("call %d saw %v, want the pass-through unchanged", i, seen)
		}
	}
}
