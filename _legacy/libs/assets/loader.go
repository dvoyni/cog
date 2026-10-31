package assets

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
)

// Loader is the plugin-side half of a cache: it decodes bytes into T, supplies
// the value for an asset that did not load, and releases one.
//
// It is stateless and long-lived. Built once, at plugin construction, it
// outlives every handler, so everything lock-bound arrives per call: the
// kernel, because retaining one past the handler that received it is a bug; the
// filesystem, because it is held under a read lock and is never retained past
// the handler's lock scope; and userData, the plugin-defined pass-through
// carrying whatever the loader needs that only a handler holds. A loader stores
// none of the three.
//
// Everything finishes inside Load. T is immutable and nothing is ever pending
// in the cache, so a plugin that wants asynchrony puts it in front of Get and
// keeps its own not-yet-requested state.
//
// Free must not call Free on the cache it belongs to. Re-entering a different
// cache is allowed and expected - a face loader freeing the font file it was
// baked from - and re-entering its own cache with a Get is allowed too. Only
// freeing its own is out, and this is the one rule the Library cannot enforce.
type Loader[P comparable, U any, T any] interface {
	// Load decodes data into a value. The Library has already performed the
	// read for a path-named asset, so data is the file's bytes; for a
	// blob-named one it is the caller's own. fsys is here so a loader that
	// needs more than one file - a shader reading its root plus the includes
	// its own contents name - can open the rest.
	//
	// k is for the loader's own faults. The Library owns the read failure;
	// everything a loader itself finds wrong - a missing texture inside a
	// model, a primitive with no declared bounds - goes to the kernel as an
	// error at the point it is produced, and the consumer decides which of
	// them is fatal.
	//
	// Whatever it returns is cached, a failure included.
	Load(k kernel.Kernel, data Blob, params P, fsys fs.FS, userData U) T

	// Default supplies the value for an asset whose read failed. It takes the
	// descriptor so a placeholder can match the shape that was asked for, and
	// it never sees the error: the Library has already reported it, and what
	// gets drawn is a separate question from what gets said.
	//
	// It is a method rather than a field so it can ask a device for its value
	// at call time rather than being built before the backend exists. A default
	// is only sometimes a picture - a null object is often the right answer,
	// with the loudness coming from the report rather than from the pixels.
	Default(d Descr[P], userData U) T

	// Free releases a value the cache is dropping. T is immutable and holds
	// backend handles, so dropping an entry without telling the loader leaks
	// the texture, the buffer or the atlas slot.
	//
	// It is called after the entry has left the table, so a loader that reaches
	// back into its own cache with a Get sees the entry already gone.
	Free(value T, userData U)
}
