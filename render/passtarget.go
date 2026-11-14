package render

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// passTarget is a target of a pass as found: the view drawn into, and the
// size at the mip level viewed, the sample count and the format of its
// texture.
type passTarget struct {
	view                       gfx.TextureView
	width, height, sampleCount uint32
	format                     TextureFormat
}

// makePassTarget answers the target view draws into, at mipLevel of a
// texture gfx made from descriptor.
func makePassTarget(view gfx.TextureView, descriptor gfx.TextureDescriptor, mipLevel uint32) passTarget {
	return passTarget{
		view:        view,
		width:       max(descriptor.Size.Width>>mipLevel, 1),
		height:      max(descriptor.Size.Height>>mipLevel, 1),
		sampleCount: descriptor.SampleCount,
		format:      TextureFormat(descriptor.Format),
	}
}

// checkAgreement answers why t is refused beside firstTarget, the first target
// found of its pass: another size or sample count. A zero firstTarget
// agrees.
func (t passTarget) checkAgreement(firstTarget passTarget) error {
	if firstTarget.view != (gfx.TextureView{}) && (t.width != firstTarget.width || t.height != firstTarget.height || t.sampleCount != firstTarget.sampleCount) {
		return fmt.Errorf("is %dx%d with %d samples, and the pass's first target %dx%d with %d", t.width, t.height, t.sampleCount, firstTarget.width, firstTarget.height, firstTarget.sampleCount)
	}
	return nil
}
