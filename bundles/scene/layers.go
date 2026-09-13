package scene

// LayerMask selects which cameras see a recorded item. A camera draws an item
// iff layers & CullMask is non-zero.
//
// Zero reads as LayersAll on both sides, so the degenerate frame — one camera,
// one box, no mask written anywhere — works without the caller learning about
// layers at all.
type LayerMask uint32

// LayersAll is every layer, which is also what a mask nobody wrote means.
const LayersAll LayerMask = ^LayerMask(0)

// layerCount is the number of distinct layers a LayerMask holds.
const layerCount = 32

// Layer is the mask of one layer. There are 32 of them; an index past the end
// wraps rather than silently becoming zero, which would read as every layer.
func Layer(i uint) LayerMask { return 1 << (i % layerCount) }

// drawnBy reports whether a camera with the given cull mask draws this item.
func (l LayerMask) drawnBy(cull LayerMask) bool {
	return l.orAll()&cull.orAll() != 0
}

func (l LayerMask) orAll() LayerMask {
	if l == 0 {
		return LayersAll
	}
	return l
}
