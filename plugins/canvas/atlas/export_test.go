package atlas

import "github.com/dvoyni/cog/plugins/render"

// FindStagedCell answers the cell staged at index among those still to
// record, and the bytes its copy writes there, row by row with no padding:
// what a test reads in place of the cell's texels, which the recording
// adapter's textures do not hold.
func (a *Atlas) FindStagedCell(index int) (render.TextureRegion, []byte) {
	pending := a.stagedCells[index]
	rowSize := pending.cell.Width * a.texelSize
	rowPitch := roundUpToMultiple(rowSize, copyRowAlignment)
	var texels []byte
	for row := range pending.cell.Height {
		start := uint32(pending.offset) + row*rowPitch
		texels = append(texels, a.stagedBytes[start:start+rowSize]...)
	}
	return pending.cell, texels
}
