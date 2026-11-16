package render

import "github.com/dvoyni/cog/gfx"

// textureSlot is the slot of one texture: the gfx texture behind its handle,
// with the views passes drew into and parameters bound, made as first
// needed and freed with it.
type textureSlot struct {
	texture      gfx.Texture
	textureViews []internedTextureView
}

// findTextureView answers s's texture view of descriptor, made the first
// time.
func (s *textureSlot) findTextureView(device gfx.Device, descriptor gfx.TextureViewDescriptor) (gfx.TextureView, error) {
	for _, view := range s.textureViews {
		if view.descriptor == descriptor {
			return view.view, nil
		}
	}
	view, err := device.CreateTextureView(s.texture, descriptor)
	if err != nil {
		return gfx.TextureView{}, err
	}
	s.textureViews = append(s.textureViews, internedTextureView{descriptor: descriptor, view: view})
	return view, nil
}

// releaseTexture releases s's gfx texture and its views, forgetting the
// views.
func (s *textureSlot) releaseTexture(device gfx.Device) {
	for _, view := range s.textureViews {
		device.ReleaseTextureView(view.view)
	}
	device.ReleaseTexture(s.texture)
	s.textureViews = s.textureViews[:0]
}

// internedTextureView is a view of a texture a textureSlot holds, and the
// descriptor it was made of: one mip level and one layer a pass draws into
// or render's mip shader samples, or the view a parameter binds.
type internedTextureView struct {
	descriptor gfx.TextureViewDescriptor
	view       gfx.TextureView
}
