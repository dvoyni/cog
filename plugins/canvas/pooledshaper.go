package canvas

import "sync"

// pooledShaper is a shaper of the font library's pool, acquired the first
// time a layout needs one, to read a run's markup, encode its spans or shape
// it, and released once the layout is done with its scratch, so a run of a
// plain string found in the layout cache takes none.
type pooledShaper struct {
	pool   *sync.Pool
	shaper *textShaper
}

// acquire answers the shaper, taken from the pool the first time it is
// asked for.
func (p *pooledShaper) acquire() *textShaper {
	if p.shaper == nil {
		p.shaper = p.pool.Get().(*textShaper)
	}
	return p.shaper
}

// release gives the shaper back to the pool, if one was acquired; what was
// read of its scratch is not read after.
func (p *pooledShaper) release() {
	if p.shaper != nil {
		p.pool.Put(p.shaper)
		p.shaper = nil
	}
}
