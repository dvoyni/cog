package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// countingMinter hands out ids without a backend, which is all a descriptor
// needs to be minted.
type countingMinter struct{ texture, buffer int }

func (m *countingMinter) NewTexture() types.TextureID { m.texture++; return types.TextureID(m.texture) }
func (m *countingMinter) NewBuffer() types.BufferID   { m.buffer++; return types.BufferID(m.buffer) }
func (m *countingMinter) Ready() bool                 { return true }

// A descriptor is the request, and an allocation's layer count is part of the
// request - unlike a path load's decoded size, which only the file knows. It is
// carried so a draw can tell a single-layer texture from an array one without
// asking the backend, which is the only other place the count survives.
//
// Zero is not "one". It means the descriptor cannot say, which is what
// BakedTexture produces: an id and nothing behind it. A check that read zero as
// one would refuse an array texture for being flat.
func TestATextureDescriptorReportsTheLayersItWasAskedFor(t *testing.T) {
	queue := NewResourceQueue(func() IDMinter { return &countingMinter{} })

	if got := queue.AllocateTexture(16, 16, 4, FormatRGBA8Srgb).Layers(); got != 4 {
		t.Errorf("AllocateTexture layers = %d, want 4", got)
	}
	if got := queue.AllocateRenderTarget(16, 16, 3, FormatRGBA8Srgb).Layers(); got != 3 {
		t.Errorf("AllocateRenderTarget layers = %d, want 3", got)
	}
	// A bake genuinely is one layer: BakeTexture takes a single pixel run and
	// there is no op that gives it more.
	if got := queue.BakeTexture(1, 1, FormatRGBA8Srgb, []byte{255, 255, 255, 255}, true, false).Layers(); got != 1 {
		t.Errorf("BakeTexture layers = %d, want 1", got)
	}
	if got := BakedTexture(7, 16, 16).Layers(); got != 0 {
		t.Errorf("BakedTexture layers = %d, want 0 for unknown", got)
	}
	if got := TextureWithResource("sprites/hero.png").Layers(); got != 0 {
		t.Errorf("TextureWithResource layers = %d, want 0 for unknown", got)
	}
}
