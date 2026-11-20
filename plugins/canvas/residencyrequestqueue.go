package canvas

import (
	"image"
	"io/fs"
	"sync"

	"github.com/dvoyni/cog/plugins/render"
)

// residencyRequestQueue is what Assets asked of the images' residency: the
// paths to unload, in the order asked, which the renderer applies as it
// next draws; the sources preloaded, each by the key of its file, held
// until an image of it is packed, so its packing's acquire shares what
// render decoded; and the files of converted images preloaded, decoded,
// each by its key, held until a converted image of it is packed, so its
// packing decodes nothing. Any task holding Assets adds to it, so its
// mutex guards them all.
type residencyRequestQueue struct {
	mutex            sync.Mutex
	unloadedPaths    []string
	preloadedSources map[sourceKey]render.Texture
	preloadedFiles   map[sourceKey]image.Image
}

// requestPreload acquires the source of key, an image's file, through
// inventory, held until an image of it is packed, unless one is preloaded
// already. The acquire reads the file's
// header outside the mutex, so other requests do not wait for it.
func (q *residencyRequestQueue) requestPreload(inventory render.Inventory, key sourceKey) {
	q.mutex.Lock()
	_, preloaded := q.preloadedSources[key]
	q.mutex.Unlock()
	if preloaded {
		return
	}
	source := inventory.AcquireTexture(key.path, key.makeTextureOptions())
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, preloaded := q.preloadedSources[key]; preloaded {
		inventory.ReleaseTexture(source)
		return
	}
	if q.preloadedSources == nil {
		q.preloadedSources = map[sourceKey]render.Texture{}
	}
	q.preloadedSources[key] = source
}

// requestFilePreload reads and decodes the file of key, a converted image's,
// from files, held until a converted image of it is packed, unless one is
// preloaded already; one that cannot be decoded is not held. The decode
// runs outside the mutex, so other requests do not wait for it.
func (q *residencyRequestQueue) requestFilePreload(files fs.FS, key sourceKey) {
	q.mutex.Lock()
	_, preloaded := q.preloadedFiles[key]
	q.mutex.Unlock()
	if preloaded {
		return
	}
	picture := decodeImageFile(files, key.path)
	if picture == nil {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, preloaded := q.preloadedFiles[key]; preloaded {
		return
	}
	if q.preloadedFiles == nil {
		q.preloadedFiles = map[sourceKey]image.Image{}
	}
	q.preloadedFiles[key] = picture
}

// takePreloadedFile answers the file preloaded of key, decoded, which it
// forgets, or nil when none was preloaded.
func (q *residencyRequestQueue) takePreloadedFile(key sourceKey) image.Image {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	picture := q.preloadedFiles[key]
	delete(q.preloadedFiles, key)
	return picture
}

// releasePreload releases through inventory the source preloaded of key, a
// file an image of which was packed, its packing's acquire sharing it, if
// one was preloaded.
func (q *residencyRequestQueue) releasePreload(inventory render.Inventory, key sourceKey) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if source, preloaded := q.preloadedSources[key]; preloaded {
		inventory.ReleaseTexture(source)
		delete(q.preloadedSources, key)
	}
}

// requestUnload queues the unload of the paths Unload(unloaded) unloads,
// releasing through inventory the sources preloaded of their images, and
// forgetting the files of their converted images preloaded.
func (q *residencyRequestQueue) requestUnload(inventory render.Inventory, unloaded string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.unloadedPaths = append(q.unloadedPaths, unloaded)
	for key, source := range q.preloadedSources {
		if isUnder(key.path, unloaded) {
			inventory.ReleaseTexture(source)
			delete(q.preloadedSources, key)
		}
	}
	for key := range q.preloadedFiles {
		if isUnder(key.path, unloaded) {
			delete(q.preloadedFiles, key)
		}
	}
}
