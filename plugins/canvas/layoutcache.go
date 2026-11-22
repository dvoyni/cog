package canvas

import (
	"sync"
	"sync/atomic"

	"github.com/go-text/typesetting/font"
)

// layoutCache is the layouts of the runs laid out lately, by what shapes
// them, so a label drawn or measured frame after frame is shaped once, and
// measuring and drawing read one layout: looked up by the jobs recording
// text and measuring it, under the FontLibrary resource's read lock, so it
// is guarded by a lock of its own. A layout unused for idleFrames frames is
// freed at the end of the frame that leaves it so.
type layoutCache struct {
	// mutex guards layouts and currentFrame; a lookup takes it for read, so
	// jobs look layouts up together.
	mutex   sync.RWMutex
	layouts map[layoutInput]*cachedLayout
	// currentFrame counts the frames ended, the frame a layout used now
	// is stamped with.
	currentFrame uint64
	idleFrames   uint64
}

// layoutInput is what a run's layout is made of, which the shaper reads
// and the cache keys by: its text; the font its set picks for the current
// language, so a set destroyed or a language changed lays out anew and the
// layouts of the font before go idle; its em in world units, Config.TextSize
// when its Size is zero; how it wraps, aligns and spaces its lines and
// letters, each as its Text gives it; and the language it is shaped in.
type layoutInput struct {
	text       string
	font       *font.Font
	em         float32
	wrapWidth  float32
	align      TextAlign
	lineHeight float32
	tracking   float32
	language   string
}

// cachedLayout is a layout the cache holds: the layout, its glyphs all of
// the run's; the glyphs it draws, each assigned its field, made the first
// time it is drawn and nil until then; and the frame it was last used in.
type cachedLayout struct {
	layout        TextLayout
	drawnGlyphs   atomic.Pointer[[]placedGlyph]
	lastUsedFrame atomic.Uint64
}

// find answers the layout of input cached, stamped as used this frame, or
// nil when none is. Only the first use in a frame writes the stamp, so jobs
// drawing one label share its stamp read-only.
func (c *layoutCache) find(input layoutInput) *cachedLayout {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	cached := c.layouts[input]
	if cached != nil && cached.lastUsedFrame.Load() != c.currentFrame {
		cached.lastUsedFrame.Store(c.currentFrame)
	}
	return cached
}

// add caches layout, made of input, unless a job laying it out at once
// cached one first, and answers the layout cached, stamped as used this
// frame.
func (c *layoutCache) add(input layoutInput, layout TextLayout) *cachedLayout {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	cached := c.layouts[input]
	if cached == nil {
		cached = &cachedLayout{layout: layout}
		c.layouts[input] = cached
	}
	cached.lastUsedFrame.Store(c.currentFrame)
	return cached
}

// endFrame frees the layouts that have now gone unused for the cache's
// idle frames, and starts the next frame.
func (c *layoutCache) endFrame() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for key, cached := range c.layouts {
		if c.currentFrame-cached.lastUsedFrame.Load() >= c.idleFrames {
			delete(c.layouts, key)
		}
	}
	c.currentFrame++
}
