package canvas

// CachedLayout is a layout the layout cache holds, for canvas's tests to
// tell a layout kept from one shaped again.
type CachedLayout = cachedLayout

// FindCachedLayout answers the layout the layout cache of fonts holds for
// text at the current language, nil when it holds none, without using it,
// for canvas's tests to see a layout kept or freed.
func FindCachedLayout(fonts FontLibrary, text Text) *CachedLayout {
	library := fonts.(*fontLibrary)
	input, _ := library.makeLayoutInput(text)
	library.layouts.mutex.RLock()
	defer library.layouts.mutex.RUnlock()
	return library.layouts.layouts[input]
}

// CountGlyphs answers how many glyphs the glyph table of fonts holds,
// drawn or preloaded, for canvas's tests to see a glyph made.
func CountGlyphs(fonts FontLibrary) int {
	library := fonts.(*fontLibrary)
	library.glyphs.mutex.Lock()
	defer library.glyphs.mutex.Unlock()
	return len(library.glyphs.fields)
}
