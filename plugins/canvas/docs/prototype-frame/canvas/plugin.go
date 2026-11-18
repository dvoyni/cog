package canvas

// Config is canvas's part of the kernel's config.
type Config struct {
	// DefaultFilter is the filter of an entry whose Filter is zero; its own zero
	// is FilterLinear. A pixel-art game sets FilterNearest once here.
	DefaultFilter Filter
	// RegroupedLayers are the layers whose entries of equal keys canvas may
	// regroup by batch, for a UI or particles whose order within a key does
	// not matter.
	RegroupedLayers []int32
	// Language is the current language at start, a tag such as "en" or "ja".
	Language string
	// DefaultFontSet is the zero FontSet's; its zero is JetBrains Mono alone.
	DefaultFontSet FontSetDescriptor
	// TextSize is the size of a run whose Size is zero, in world units; zero
	// means 16.
	TextSize float32
	// LayoutIdleFrames is how many frames DrawText keeps a layout no draw
	// uses; zero means 2.
	LayoutIdleFrames int
	// DoNotGenerateMipMaps leaves the sprite atlas's pages without mips, as a
	// pixel-perfect game wants; MipLevels fixes how many each page has, zero
	// meaning 3, and so its gutter and grid.
	DoNotGenerateMipMaps bool
	MipLevels            uint32
}
