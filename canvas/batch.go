package canvas

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// spriteBatch accumulates consecutive batchable sprite/glyph instances that share
// an atlas texture, layer transform, clip, and sampler filter, then emits them as
// a single instanced draw. Flushing on any key change preserves draw order.
type spriteBatch struct {
	active    bool
	texture   gfx.TextureDescr
	textureID gfx.TextureID
	layer     m.Mat4
	clip      m.Rect
	hasClip   bool
	filter    gfx.FilterMode
	viewport  m.Vec2
	instances []SpriteInstance
}

func (b *spriteBatch) keyMatches(texture gfx.TextureDescr, layer m.Mat4, clip m.Rect, hasClip bool, filter gfx.FilterMode) bool {
	return b.textureID == texture.ID() && b.layer == layer && b.clip == clip &&
		b.hasClip == hasClip && b.filter == filter
}

func (b *spriteBatch) add(gfxWrite *gfx.OpQueue, quad gfx.MeshDescr, texture gfx.TextureDescr, layer m.Mat4, clip m.Rect, hasClip bool, filter gfx.FilterMode, viewport m.Vec2, t0, t1, frame, tint, misc, keyColor m.Vec4) {
	if b.active && !b.keyMatches(texture, layer, clip, hasClip, filter) {
		b.flush(gfxWrite, quad)
	}
	if !b.active {
		b.active = true
		b.texture = texture
		b.textureID = texture.ID()
		b.layer = layer
		b.clip = clip
		b.hasClip = hasClip
		b.filter = filter
		b.viewport = viewport
		b.instances = b.instances[:0]
	}
	b.instances = append(b.instances, SpriteInstance{
		Transform0: t0, Transform1: t1, Frame: frame, Tint: tint, Misc: misc, KeyColor: keyColor,
	})
}

func (b *spriteBatch) flush(gfxWrite *gfx.OpQueue, quad gfx.MeshDescr) {
	if !b.active || len(b.instances) == 0 {
		b.active = false
		b.instances = b.instances[:0]
		return
	}
	clipEnabled := float32(0)
	if b.hasClip {
		clipEnabled = 1
	}
	buffer := gfx.BufferWithBytes(spriteInstanceBytes(b.instances), true)
	gfxWrite.DrawInstanced(quad, defaultSpriteBatchMaterial, len(b.instances),
		gfx.VecParam("canvasViewport", m.Vec4{X: b.viewport.X, Y: b.viewport.Y, Z: clipEnabled}),
		gfx.MatParam("canvasLayer", b.layer),
		gfx.VecParam("canvasClip", m.Vec4{X: b.clip.X, Y: b.clip.Y, Z: b.clip.X + b.clip.Width, W: b.clip.Y + b.clip.Height}),
		gfx.BufferParam("instances", buffer),
		gfx.TextureParam(TextureSlot, b.texture),
		gfx.SamplerParam(SamplerSlot, gfx.AddressClamp, b.filter),
	)
	b.active = false
	b.instances = b.instances[:0]
}

// batchEntry computes one sprite/glyph instance and adds it to the batcher.
func (p *Plugin) batchEntry(gfxWrite *gfx.OpQueue, view *app.Viewport, entry atlasEntry, transform SpriteTransform, layerTransform m.Mat4, clip m.Rect, hasClip bool, tint m.Color, keyColor m.Color) {
	size := entrySize(entry, transform)
	if size.X == 0 || size.Y == 0 {
		return
	}
	uv, ok := entryUV(entry, transform)
	if !ok {
		return
	}
	if hasClip && (clip.Width <= 0 || clip.Height <= 0) {
		return
	}
	sine, cosine := sincos(transform.Rotation)
	t0 := m.Vec4{X: transform.Position.X, Y: transform.Position.Y, Z: size.X, W: size.Y}
	t1 := m.Vec4{X: transform.Origin.X, Y: transform.Origin.Y, Z: sine, W: cosine}
	misc := m.Vec4{X: float32(entry.layer)}
	p.batch.add(gfxWrite, p.quad, entry.texture, layerTransform, clip, hasClip, transform.Filter,
		m.Vec2{X: view.Width, Y: view.Height}, t0, t1, uv, colorVec(tint), misc, colorVec(keyColor))
}

// trianglesBatch concatenates the vertices of consecutive default-material
// DrawTriangles ops that share a texture, sampler, keying, layer transform, clip,
// and vertex layout, emitting them as one draw. This collapses the game's many
// small textured-quad ops (tiles, borders, walls, sprites) into far fewer draws.
type trianglesBatch struct {
	active      bool
	layoutID    int
	layout      []gfx.VertexAttr
	texture     gfx.TextureDescr
	texturePath string
	textureID   gfx.TextureID
	hasTexture  bool
	sampler     gfx.SamplerDesc
	keyColor    m.Color
	layer       m.Mat4
	clip        m.Rect
	hasClip     bool
	viewport    m.Vec2
	vertices    []byte
}

func (b *trianglesBatch) keyMatches(layoutID int, texture gfx.TextureDescr, hasTexture bool, sampler gfx.SamplerDesc, keyColor m.Color, layer m.Mat4, clip m.Rect, hasClip bool) bool {
	return b.layoutID == layoutID && b.hasTexture == hasTexture &&
		b.texturePath == texture.Path() && b.textureID == texture.ID() && b.sampler == sampler &&
		b.keyColor == keyColor &&
		b.layer == layer && b.clip == clip && b.hasClip == hasClip
}

func (b *trianglesBatch) add(gfxWrite *gfx.OpQueue, viewport m.Vec2, layoutID int, layout []gfx.VertexAttr, texture gfx.TextureDescr, hasTexture bool, sampler gfx.SamplerDesc, keyColor m.Color, layer m.Mat4, clip m.Rect, hasClip bool, vertices []byte) {
	if b.active && !b.keyMatches(layoutID, texture, hasTexture, sampler, keyColor, layer, clip, hasClip) {
		b.flush(gfxWrite)
	}
	if !b.active {
		b.active = true
		b.layoutID = layoutID
		b.layout = layout
		b.texture = texture
		b.texturePath = texture.Path()
		b.textureID = texture.ID()
		b.hasTexture = hasTexture
		b.sampler = sampler
		b.keyColor = keyColor
		b.layer = layer
		b.clip = clip
		b.hasClip = hasClip
		b.viewport = viewport
		b.vertices = b.vertices[:0]
	}
	b.vertices = append(b.vertices, vertices...)
}

func (b *trianglesBatch) flush(gfxWrite *gfx.OpQueue) {
	if !b.active || len(b.vertices) == 0 {
		b.active = false
		b.vertices = b.vertices[:0]
		return
	}
	clipEnabled := float32(0)
	if b.hasClip {
		clipEnabled = 1
	}
	params := make([]gfx.ParameterDescr, 0, 6)
	params = append(params,
		gfx.VecParam("canvasViewport", m.Vec4{X: b.viewport.X, Y: b.viewport.Y, Z: clipEnabled}),
		gfx.MatParam("canvasLayer", b.layer),
		gfx.VecParam("canvasClip", m.Vec4{X: b.clip.X, Y: b.clip.Y, Z: b.clip.X + b.clip.Width, W: b.clip.Y + b.clip.Height}),
	)
	if b.hasTexture {
		params = append(params,
			gfx.TextureParam(TextureSlot, b.texture),
			gfx.SamplerParam(SamplerSlot, b.sampler.Address, b.sampler.Filter),
		)
	}
	params = append(params, gfx.ColorParam("keyColor", b.keyColor))
	mesh := gfx.Mesh(gfx.BufferWithBytes(b.vertices, true), gfx.TopologyTriangleList, b.layout...)
	gfxWrite.Draw(mesh, defaultTrianglesMaterial, params...)
	b.active = false
	b.vertices = b.vertices[:0]
}
