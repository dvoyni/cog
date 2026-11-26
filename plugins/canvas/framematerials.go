package canvas

// frameMaterials are the slots of the 2D materials a frame draws with: the
// default material's, SDFMaterial's and TextMaterial's, zero until made at
// canvas's first draw, and those of the materials Assets made, by index, as
// they stood as the frame began drawing, written over each frame.
type frameMaterials struct {
	defaultSlot materialSlot
	sdfSlot     materialSlot
	textSlot    materialSlot
	slots       []materialSlot
}

// find answers the slot of material, and whether canvas holds it: the
// default material's for the zero Material, SDFMaterial's and
// TextMaterial's, and a released material's until its slot is freed, once
// the frame of its release is drawn.
func (f *frameMaterials) find(material Material) (*materialSlot, bool) {
	switch material {
	case Material{}:
		return &f.defaultSlot, true
	case SDFMaterial:
		return &f.sdfSlot, true
	case TextMaterial:
		return &f.textSlot, true
	}
	return findSlot(f.slots, material)
}
