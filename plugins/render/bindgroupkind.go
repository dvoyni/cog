package render

import (
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/render/internal/handle"
)

// BindGroupKind is what a bind group binds.
type BindGroupKind uint8

const (
	// BindGroupKindParameters is a bind group of a pass's, a draw's or a
	// compute pass's parameters, shared by every draw or dispatch binding
	// the same.
	BindGroupKindParameters BindGroupKind = iota + 1
	// BindGroupKindMaterial is a material's own bind group of its
	// parameters.
	BindGroupKindMaterial
	// BindGroupKindDefault is the bind group of a group of a shader's
	// layout nothing is set to, every binding drawing its default.
	BindGroupKindDefault
	// BindGroupKindMips is a bind group of render's mip generation.
	BindGroupKindMips
)

// createBindGroup makes a bind group of k of layout binding entries,
// labelled as render labels a bind group of k, so a reader of gfx's calls
// tells its kind, and writes it down in inspector's inspection.
func (k BindGroupKind) createBindGroup(device gfx.Device, inspector *inspector, layout gfx.BindGroupLayout, entries []gfx.BindGroupEntry) (gfx.BindGroup, error) {
	label := handle.ParameterBindGroupLabel
	switch k {
	case BindGroupKindMaterial:
		label = handle.MaterialBindGroupLabel
	case BindGroupKindDefault:
		label = handle.DefaultBindGroupLabel
	case BindGroupKindMips:
		label = handle.MipBindGroupLabel
	}
	group, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Label: label, Layout: layout, Entries: entries})
	if err == nil {
		inspector.add(func(inspection *Inspection) {
			inspection.MadeBindGroups = append(inspection.MadeBindGroups, InspectedBindGroup{Kind: k})
		})
	}
	return group, err
}
