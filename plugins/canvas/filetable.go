package canvas

import (
	"fmt"
	"image"
	"io/fs"
	"slices"
	"sync"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// fileTable is what canvas keeps of each image file and mesh file it used,
// by its path, until Unload unloads the path: its stamp, its header's size
// as ImageSize read it, its preloads, and the keys of the reports raised
// for it, so unloading a path forgets all of them at once, whatever report
// a later change raises. It queues the paths unloaded for the drawing job,
// which unloads what it packed and loaded of them as it next draws. The
// drawing job and any task holding Assets use it, so its mutex guards it
// all; a file is read or decoded outside it, so other calls do not wait for
// the read, and what was read is kept only when no other call kept it
// first.
type fileTable struct {
	mutex         sync.RWMutex
	usedFiles     map[string]*usedFile
	unloadedPaths []string
}

// stamp stamps the file at path in files, at its first use, unless it is
// stamped already, statting it outside the mutex: of two calls stamping it
// at once, the later's stamp is kept, either taken as it was read. It is
// called before the file is read, so a change made as it is read is seen
// by the next comparison.
func (t *fileTable) stamp(files fs.FS, path string) {
	t.mutex.RLock()
	used, found := t.usedFiles[path]
	stamped := found && used.stamped
	t.mutex.RUnlock()
	if stamped {
		return
	}
	stamp := readFileStamp(files, path)
	t.mutex.Lock()
	defer t.mutex.Unlock()
	used = t.findUsedFile(path)
	used.stamp = stamp
	used.stamped = true
}

// findSize answers the size of the image file at path in files, in texels,
// and whether its header was read: from the table, or else read from the
// header, the file stamped first, and kept, the first kept when two calls
// read it. A header that cannot be read is kept as unread, and reported,
// once for path, as ReportKindImageHeaderUnread.
func (t *fileTable) findSize(files fs.FS, reporter *reporter, path string) (m.Vec2, bool) {
	t.mutex.RLock()
	used, found := t.usedFiles[path]
	if found && used.sized {
		size := used.size
		t.mutex.RUnlock()
		return size.size, size.read
	}
	t.mutex.RUnlock()
	t.stamp(files, path)
	config, err := readImageConfig(files, path)
	var size headerSize
	if err == nil {
		size = headerSize{size: m.Vec2{X: float32(config.Width), Y: float32(config.Height)}, read: true}
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	used = t.findUsedFile(path)
	if used.sized {
		return used.size.size, used.size.read
	}
	if err != nil {
		unread := Report{Kind: ReportKindImageHeaderUnread, Err: fmt.Errorf("canvas: reading the header of image %q: %w", path, err)}
		t.raise(reporter, used, imageHeaderUnreadLogKey+path, unread)
	}
	used.size = size
	used.sized = true
	return size.size, size.read
}

// preload preloads the file of key, unless it is preloaded already: a
// converted image's read from files and decoded, the file stamped first,
// one that cannot be decoded not held; any other's acquired through
// textures. The read or the acquire runs outside the mutex, and a texture
// acquired as another call preloaded it is released.
func (t *fileTable) preload[H textureInventory](textures H, files fs.FS, key sourceKey, converted bool) {
	t.mutex.RLock()
	used, found := t.usedFiles[key.path]
	preloaded := found && used.holdsPreload(key.colorSpace, converted)
	t.mutex.RUnlock()
	if preloaded {
		return
	}
	if converted {
		t.stamp(files, key.path)
		picture := decodeImageFile(files, key.path)
		if picture == nil {
			return
		}
		t.mutex.Lock()
		defer t.mutex.Unlock()
		used = t.findUsedFile(key.path)
		if !used.holdsPreload(key.colorSpace, true) {
			if used.pictures == nil {
				used.pictures = map[ColorSpace]image.Image{}
			}
			used.pictures[key.colorSpace] = picture
		}
		return
	}
	texture := textures.AcquireTexture(key.path, key.makeTextureOptions())
	t.mutex.Lock()
	defer t.mutex.Unlock()
	used = t.findUsedFile(key.path)
	if used.holdsPreload(key.colorSpace, false) {
		textures.ReleaseTexture(texture)
		return
	}
	if used.textures == nil {
		used.textures = map[ColorSpace]render.Texture{}
	}
	used.textures[key.colorSpace] = texture
}

// takePreload takes the preload of key, which it forgets: a converted
// image's file decoded, answered, or nil when none was preloaded; any
// other's texture, released through textures as an image of it is packed,
// its packing's acquire sharing it.
func (t *fileTable) takePreload[H textureInventory](textures H, key sourceKey, converted bool) image.Image {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	used, found := t.usedFiles[key.path]
	if !found {
		return nil
	}
	if converted {
		picture := used.pictures[key.colorSpace]
		delete(used.pictures, key.colorSpace)
		return picture
	}
	if texture, held := used.textures[key.colorSpace]; held {
		textures.ReleaseTexture(texture)
		delete(used.textures, key.colorSpace)
	}
	return nil
}

// reportOnce raises report, once for path under logKey followed by path, as
// reporter.reportOnce does, recording the key under path, so unloading path
// forgets it, and the report may be raised again.
func (t *fileTable) reportOnce(reporter *reporter, path, logKey string, report Report) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.raise(reporter, t.findUsedFile(path), logKey+path, report)
}

// unload forgets what the table keeps of each path Unload(prefix) unloads,
// its stamp, size and preloads, releasing its preloaded textures through
// textures, and the reports raised for it, so they may be raised again;
// and queues prefix for the drawing job.
func (t *fileTable) unload[H textureInventory](textures H, reporter *reporter, prefix string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.unloadedPaths = append(t.unloadedPaths, prefix)
	for path, used := range t.usedFiles {
		if !isUnder(path, prefix) {
			continue
		}
		for _, texture := range used.textures {
			textures.ReleaseTexture(texture)
		}
		for _, key := range used.reportKeys {
			reporter.forget(key)
		}
		delete(t.usedFiles, path)
	}
}

// takeUnloadedPaths appends to paths the paths queued to unload, in the
// order asked, which it forgets, and answers paths appended to.
func (t *fileTable) takeUnloadedPaths(paths []string) []string {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	paths = append(paths, t.unloadedPaths...)
	clear(t.unloadedPaths)
	t.unloadedPaths = t.unloadedPaths[:0]
	return paths
}

// findChangedPaths answers, sorted, the paths stamped whose files in files
// changed since they were stamped, statting each outside the mutex, so
// other calls do not wait for the stats, and reading none.
func (t *fileTable) findChangedPaths(files fs.FS) []string {
	// stampedPath is a path stamped and its stamp, as the mutex held them.
	type stampedPath struct {
		path  string
		stamp fileStamp
	}
	t.mutex.RLock()
	stamped := make([]stampedPath, 0, len(t.usedFiles))
	for path, used := range t.usedFiles {
		if used.stamped {
			stamped = append(stamped, stampedPath{path: path, stamp: used.stamp})
		}
	}
	t.mutex.RUnlock()
	var changed []string
	for _, file := range stamped {
		if !file.stamp.matches(files, file.path) {
			changed = append(changed, file.path)
		}
	}
	slices.Sort(changed)
	return changed
}

// findUsedFile answers what the table keeps of path, made when it keeps
// nothing of it. The mutex is held for writing.
func (t *fileTable) findUsedFile(path string) *usedFile {
	used, found := t.usedFiles[path]
	if !found {
		if t.usedFiles == nil {
			t.usedFiles = map[string]*usedFile{}
		}
		used = &usedFile{}
		t.usedFiles[path] = used
	}
	return used
}

// raise raises report, once for key, recording key under used. The mutex
// is held for writing, so an unload of its path forgets every key raised
// before it.
func (t *fileTable) raise(reporter *reporter, used *usedFile, key string, report Report) {
	if !slices.Contains(used.reportKeys, key) {
		used.reportKeys = append(used.reportKeys, key)
	}
	reporter.reportOnce(key, report)
}

// usedFile is what the table keeps of a file canvas used, by its path.
type usedFile struct {
	// stamp is the file's stamp, taken before it was first read, when
	// stamped.
	stamp   fileStamp
	stamped bool
	// size is its header's size, when sized.
	size  headerSize
	sized bool
	// textures are the textures of it preloaded, by colour space, acquired
	// from render and held until an image of it is packed, so its packing's
	// acquire shares what render decoded; pictures are the file decoded, by
	// colour space, preloaded for a converted image and held until a
	// converted image of it is packed, so its packing decodes nothing.
	textures map[ColorSpace]render.Texture
	pictures map[ColorSpace]image.Image
	// reportKeys are the keys of the reports raised for it.
	reportKeys []string
}

// holdsPreload answers whether f holds a preload in colorSpace: a decoded
// file when converted, else a texture.
func (f *usedFile) holdsPreload(colorSpace ColorSpace, converted bool) bool {
	if converted {
		_, held := f.pictures[colorSpace]
		return held
	}
	_, held := f.textures[colorSpace]
	return held
}

// textureInventory is the part of render's Inventory the table acquires
// and releases preloaded textures through, met by the Inventory itself or
// a test's counter. The methods taking it are generic over it, so an
// Inventory passed is not boxed.
type textureInventory interface {
	AcquireTexture(path string, options render.TextureOptions) render.Texture
	ReleaseTexture(texture render.Texture)
}

// headerSize is an image file's size in texels, as its header gives it,
// and whether its header was read.
type headerSize struct {
	size m.Vec2
	read bool
}

// readImageConfig answers the image file at path in files' header: its
// size and colour model, read with image.DecodeConfig, its body unread. An
// image of no texels is an error, as render's loads take it.
func readImageConfig(files fs.FS, path string) (image.Config, error) {
	file, err := files.Open(path)
	if err != nil {
		return image.Config{}, err
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err == nil && (config.Width <= 0 || config.Height <= 0) {
		err = fmt.Errorf("an image of %d×%d texels", config.Width, config.Height)
	}
	return config, err
}
