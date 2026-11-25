// Package atlas packs images of one format into the pages of one 2D-array
// texture, so entries drawing any of them bind one texture and can be drawn
// together. canvas's sprites and glyphs are packed into atlases of it, and a
// plugin of its own, such as a UI caching its rasterised text, packs its own
// images the same way, drawn through a 2D material of its own that binds the
// atlas's texture.
//
// An Atlas is one atlas: a 2D-array texture made through render's Inventory,
// each layer a page, holding texels of one format, whose meaning is the
// shader's business. It knows no image, converter or eviction: what lies
// where, what to evict and when to free it are its user's to keep.
//
// Each image is packed as a cell of its own: the image rounded up to the
// atlas grid, 2^(MipLevelCount−1) texels, then its gutter on every side,
// which Put fills from the image's own edges so filtering and mips never
// reach a neighbour. Every cell lies on the atlas grid, so each mip level
// halves it exactly and a page's mips mix no cells.
//
// A frame using an atlas packs, then records, in one command list before any
// draw sampling it:
//
//	region, err := glyphs.Pack(inventory, width, height)
//	if err == nil {
//		glyphs.Put(region, texels, atlas.WrapNone)
//	}
//	glyphs.RecordCopies(&list, inventory)
//	// passes filling cells on the GPU go here, into glyphs.Cell(region)
//	glyphs.RecordMips(&list)
//
// then binds glyphs.Texture() for its draws, read as it binds, since a growth
// replaces it, and places a region's texels with TextureRect and its page,
// region.Layer.
//
// An Atlas is not safe for concurrent use: one task at a time packs, puts and
// records it.
package atlas
