package assets

import (
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/kernel"
)

// Cache is one table of entries for one asset family, generic in the bake
// parameters P, the plugin-defined pass-through U and the loaded value T. A
// plugin holds several: a model table and a texture table are two instances,
// not two kinds.
//
// It has four verbs - Get, Free, FreeAll and FreeWhere - and every one of them
// either loads or frees. There is no fifth. It is not a probe: Get is the only
// read and it loads on a miss, so there is no is this loaded? and no what is
// this, without loading it. It is not a walk, it does not refcount, it has no
// budget and evicts nothing by itself, it watches nothing, and it does not know
// what a frame is.
//
// The entry is the once. Whatever a load returns is cached, a failure included,
// so a load runs one time per key until a Free - which is where terminal
// failure, report-once and the absence of a retry all come from together.
//
// A Cache is not internally synchronised. It is an ordinary engine table: it
// lives in a resource, and every verb runs under whatever lock its holder was
// granted, which is also why the kernel and the filesystem arrive per call.
type Cache[P comparable, U any, T any] struct {
	loader  Loader[P, U, T]
	entries map[Descr[P]]T
}

// New builds an empty cache over loader.
func New[P comparable, U any, T any](loader Loader[P, U, T]) *Cache[P, U, T] {
	return &Cache[P, U, T]{loader: loader, entries: map[Descr[P]]T{}}
}

// Get returns the asset d names, loading it if the table has no entry for it.
//
// The Library reads and the loader decodes: a descriptor with a Name and no
// payload is opened through fsys and its bytes handed to Load. A descriptor
// carrying a Blob skips the read either way, because the bytes are already in
// hand - and beside a Name that is the whole point of a payload. An asset that
// lives inside a container the caller has already parsed is named by that
// container and read from nobody, so the Library never opens the container to
// reach it and the loader never re-parses it. Neither k nor fsys is retained
// past the call.
//
// A read that fails is the Library's own to report, and it reports it once,
// keyed by the descriptor itself. Load is not called; Default supplies the
// value, and that value is cached like any other, so the failure is terminal
// until a Free.
func (c *Cache[P, U, T]) Get(k kernel.Kernel, d Descr[P], fsys fs.FS, userData U) T {
	key := d.key()
	if value, ok := c.entries[key]; ok {
		return value
	}

	data := d.Blob
	if d.Name != "" && data.Len() == 0 {
		read, err := fs.ReadFile(fsys, d.Name)
		if err != nil {
			var zero T
			// The descriptor is the key: ReportErrorOnce takes any comparable
			// and distinct key types never collide, so Descr[P] owns a
			// namespace of its own and needs no prefix. The read error is
			// wrapped untouched, so errors.Is against fs.ErrNotExist keeps
			// working for anyone who wants the distinction the Library does
			// not draw.
			k.ReportErrorOnce(key, fmt.Errorf("asset %T not found by path %q: %w", zero, d.Name, err))
			value := c.loader.Default(d, userData)
			c.entries[key] = value
			return value
		}
		data = NewBlob(read)
	}

	value := c.loader.Load(k, data, d.Params, fsys, userData)
	c.entries[key] = value
	return value
}

// Free releases the entry d names, immediately. There is no queue: a free
// followed by a get is a reload, not an error.
//
// It is also the retry lever, which is why it takes a kernel: the entry's
// report-once key is forgotten with it, so a path that failed, was fixed and
// was asked for again can speak. Freeing an entry the table does not hold does
// nothing.
func (c *Cache[P, U, T]) Free(k kernel.Kernel, d Descr[P], userData U) {
	key := d.key()
	value, ok := c.entries[key]
	if !ok {
		return
	}
	delete(c.entries, key)
	k.ForgetReportedError(key)
	c.loader.Free(value, userData)
}

// FreeAll releases every entry in the table, immediately.
//
// It collects and clears before it frees, so a loader that reaches back into
// its own cache inside Free cannot corrupt the walk. It walks whatever is in
// the table at the call: an asset asked for after a FreeAll and before the
// frame ends was deliberately asked for, and survives.
//
// The whole family's report-once keys go with it, which is a full scan of the
// kernel's table and belongs where this verb already belongs - an unload, not a
// frame.
func (c *Cache[P, U, T]) FreeAll(k kernel.Kernel, userData U) {
	freed := make([]T, 0, len(c.entries))
	for _, value := range c.entries {
		freed = append(freed, value)
	}
	clear(c.entries)
	k.ForgetReportedErrors(func(Descr[P]) bool { return true })

	for _, value := range freed {
		c.loader.Free(value, userData)
	}
}

// FreeWhere releases every entry match accepts, immediately, collecting and
// removing before it frees exactly as FreeAll does.
//
// It exists because a release decision can need to read the value: evict every
// shader whose sources contain this path, every face baked from this font file,
// every colour-space variant of this image. The caller cannot name those keys,
// and the value already holds the answer, so this is what a plugin-side reverse
// index would otherwise be.
//
// match is shown the descriptor as the table keys it, which is the descriptor a
// Free would name, not whatever payload a Get supplied beside it.
func (c *Cache[P, U, T]) FreeWhere(k kernel.Kernel, userData U, match func(d Descr[P], value T) bool) {
	var keys []Descr[P]
	var freed []T
	for key, value := range c.entries {
		if match(key, value) {
			keys = append(keys, key)
			freed = append(freed, value)
		}
	}
	for _, key := range keys {
		delete(c.entries, key)
		k.ForgetReportedError(key)
	}

	for _, value := range freed {
		c.loader.Free(value, userData)
	}
}
