package canvas

import (
	"sync"

	"github.com/dvoyni/cog/plugins/render"
)

// residencyRequestQueue is what Assets asked of the images' residency: the
// paths to unload, in the order asked, which the renderer applies as it
// next draws, and the sources preloaded, each by the key of the options
// that change its image's pixels, held until the image is packed, so its
// packing's acquire shares what render decoded. Any task
// holding Assets adds to it, so its mutex guards both.
type residencyRequestQueue struct {
	mutex            sync.Mutex
	unloadedPaths    []string
	preloadedSources map[imageKey]render.Texture
}

// requestPreload acquires the source of the image of key, the key of the
// options that change its pixels, through inventory, held until the image
// is packed, unless one is preloaded already. The acquire reads the file's
// header outside the mutex, so other requests do not wait for it.
func (q *residencyRequestQueue) requestPreload(inventory render.Inventory, key imageKey) {
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
		q.preloadedSources = map[imageKey]render.Texture{}
	}
	q.preloadedSources[key] = source
}

// requestUnload queues the unload of the paths Unload(unloaded) unloads,
// releasing through inventory the sources preloaded of their images.
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
}
