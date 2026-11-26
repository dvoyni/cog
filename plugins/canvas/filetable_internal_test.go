package canvas

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// TestFileTableUnloadsUnderPrefix shows the prefix rule: a folder's prefix,
// ending in a slash, unloads the paths in it and not those of a folder whose
// name it begins, and a path without one only itself.
func TestFileTableUnloadsUnderPrefix(t *testing.T) {
	files := fstest.MapFS{
		"level1":           {Data: []byte("a")},
		"level1/hero.png":  {Data: []byte("b")},
		"level10/hero.png": {Data: []byte("c")},
	}
	var table fileTable
	for path := range files {
		table.stamp(files, path)
	}
	for _, file := range files {
		file.ModTime = time.Unix(1, 0)
	}
	textures := &textureCounter{}
	table.unload(textures, nil, "level1/")
	if changed := table.findChangedPaths(files); !slices.Equal(changed, []string{"level1", "level10/hero.png"}) {
		t.Fatalf("after unloading level1/, the changed paths are %q, want level1 and level10/hero.png", changed)
	}
	table.unload(textures, nil, "level1")
	if changed := table.findChangedPaths(files); !slices.Equal(changed, []string{"level10/hero.png"}) {
		t.Fatalf("after unloading level1, the changed paths are %q, want level10/hero.png", changed)
	}
	if queued := table.takeUnloadedPaths(nil); !slices.Equal(queued, []string{"level1/", "level1"}) {
		t.Fatalf("the drawing job takes %q, want level1/ then level1", queued)
	}
	if queued := table.takeUnloadedPaths(nil); len(queued) != 0 {
		t.Fatalf("the drawing job takes %q a second time, want none", queued)
	}
}

// TestFileTableRearmsReportsOfUnloadedPaths shows a report raised for a path
// logged once, again once the path or its folder is unloaded, and not when
// a sibling's path or a folder whose name its folder's begins is.
func TestFileTableRearmsReportsOfUnloadedPaths(t *testing.T) {
	canvasReporter, logged := newRecordingReporter(t)
	var table fileTable
	report := Report{Kind: ReportKindMeshFileNotLoaded, Err: errors.New("canvas: a test's report")}
	raise := func() {
		table.reportOnce(canvasReporter, "level1/hero.mesh", meshFileNotLoadedLogKey, report)
	}
	raise()
	raise()
	textures := &textureCounter{}
	table.unload(textures, canvasReporter, "level1/villain.mesh")
	table.unload(textures, canvasReporter, "level10/")
	raise()
	if count := logged.count(); count != 1 {
		t.Fatalf("raised three times with only others unloaded between, the report is logged %d times, want 1", count)
	}
	table.unload(textures, canvasReporter, "level1/hero.mesh")
	raise()
	table.unload(textures, canvasReporter, "level1/")
	raise()
	if count := logged.count(); count != 3 {
		t.Fatalf("raised after its path was unloaded and after its folder was, the report is logged %d times, want 3", count)
	}
}

// TestFileTableFindsChangedPathsReadingNone shows a changed file's path
// found, and an unchanged one's not, by their stamps alone, opening neither.
func TestFileTableFindsChangedPathsReadingNone(t *testing.T) {
	files := fstest.MapFS{
		"changed.png":   {Data: []byte("a")},
		"unchanged.png": {Data: []byte("b")},
	}
	var table fileTable
	table.stamp(files, "changed.png")
	table.stamp(files, "unchanged.png")
	files["changed.png"].Data = []byte("ab")
	if changed := table.findChangedPaths(statOnlyFS{t: t, files: files}); !slices.Equal(changed, []string{"changed.png"}) {
		t.Fatalf("the changed paths are %q, want changed.png", changed)
	}
}

// TestFileTableReleasesPreloadsOnUnload shows a preloaded texture acquired
// once and released by its path's unload, and a converted image's file
// decoded as it was preloaded forgotten by it, so a packing after takes
// neither.
func TestFileTableReleasesPreloadsOnUnload(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"hero.png": {Data: encoded.Bytes()}}
	textures := &textureCounter{}
	var table fileTable
	key := sourceKey{path: "hero.png"}
	table.preload(textures, files, key, false)
	table.preload(textures, files, key, false)
	table.preload(textures, files, key, true)
	if textures.acquired != 1 {
		t.Fatalf("preloaded twice, the texture is acquired %d times, want 1", textures.acquired)
	}
	table.unload(textures, nil, "villain.png")
	if textures.released != 0 {
		t.Fatalf("after another path's unload, the texture is released %d times, want 0", textures.released)
	}
	table.unload(textures, nil, "hero.png")
	if textures.released != 1 {
		t.Fatalf("after its path's unload, the texture is released %d times, want 1", textures.released)
	}
	if picture := table.takePreload(textures, key, true); picture != nil {
		t.Fatal("a packing after the unload takes the converted file preloaded before it")
	}
	table.takePreload(textures, key, false)
	if textures.released != 1 {
		t.Fatalf("a packing after the unload releases the texture again, %d times in all, want 1", textures.released)
	}
}

// textureCounter counts the textures acquired and released through it.
type textureCounter struct {
	acquired, released int
}

func (r *textureCounter) AcquireTexture(string, render.TextureOptions) render.Texture {
	r.acquired++
	return render.Texture{}
}

func (r *textureCounter) ReleaseTexture(render.Texture) {
	r.released++
}

// statOnlyFS is files, statting its files and failing t at any open.
type statOnlyFS struct {
	t     *testing.T
	files fs.FS
}

func (s statOnlyFS) Open(path string) (fs.File, error) {
	s.t.Errorf("%s was opened", path)
	return s.files.Open(path)
}

func (s statOnlyFS) Stat(path string) (fs.FileInfo, error) {
	return fs.Stat(s.files, path)
}

// newRecordingReporter answers a reporter logging through a kernel of no
// plugins, and the errors it logged.
func newRecordingReporter(t *testing.T) (*reporter, *errorRecorder) {
	recorder := &errorRecorder{}
	supervisor := kernel.NewSupervisor(recorder)
	id, err := supervisor.Spawn(nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(supervisor.Quit)
	return &reporter{logger: supervisor.GetKernel(id).Logger(), inspector: &inspector{}}, recorder
}

// errorRecorder is a kernel.LogHandler keeping the errors it is told of.
type errorRecorder struct {
	mutex  sync.Mutex
	errors []error
}

func (r *errorRecorder) LogErr(_ kernel.KernelID, err error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.errors = append(r.errors, err)
}

func (r *errorRecorder) LogWarn(kernel.KernelID, string, ...any) {}

func (r *errorRecorder) LogDebug(kernel.KernelID, string, ...any) {}

// count answers how many errors r was told of.
func (r *errorRecorder) count() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return len(r.errors)
}
