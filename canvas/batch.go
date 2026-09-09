package canvas

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// spriteShading is one sprite draw's resolved shading: the material it draws
// with, the key that material contributes to its batch, and its parameters split
// by the frequency each one lands at.
//
// The split is the whole mechanism. A value parameter named at a sprite draw is
// per sprite, so it is collected across the batch into one storage array read
// with the same instance index the record uses, and two sprites differing only
// in it still merge. Everything else - a texture, a sampler, a buffer, and the
// scope's own parameters - is per batch, because a bind group is per draw and a
// scope's values do not vary within one.
type spriteShading struct {
	material    *gfx.MaterialDescr
	fingerprint uint64
	arrays      []gfx.ParameterDescr
	shared      []gfx.ParameterDescr
	sharedKey   uint64
}

// spriteBatch accumulates consecutive sprite, glyph and fill instances that
// share an atlas texture, layer transform, clip, sampler filter and shading,
// then emits them as a single instanced draw. Flushing on any key change
// preserves draw order.
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

	// material is what every sprite in the batch resolved to, and fingerprint is
	// what it contributes to the key. Whether the caller named a material is not
	// a key field: a nil material normalises to the built-in, so passing
	// DefaultMaterial() explicitly batches identically to passing nil.
	//
	// The fingerprint is compared bare, with no verification pass. A 64-bit
	// collision would merge two different materials and draw the wrong one; at
	// canvas's draw counts that is around 1e-16 a frame, and a verification
	// branch that never runs costs more than it protects.
	material    *gfx.MaterialDescr
	fingerprint uint64

	// arrayNames is the ordered list of per-instance parameter names every sprite
	// in the batch carries, arraySizes each one's element size, and arrayBytes the
	// buffer each is collected into, one element per instance.
	//
	// The name set splits the batch. Every sprite contributes exactly one element
	// to every array, so a sprite carrying a name another lacks cannot share a
	// batch with it, and the missing element is never zero-filled: for a
	// multiplier, zero is not "absent" but the opposite of it, and the failure
	// would be a silent visual bug with no error anywhere.
	//
	// The size splits it too, for the same reason one step down: one name carried
	// at two kinds would pack an array the shader strides through wrongly, and a
	// wrong stride is a wrong picture with nothing reported.
	arrayNames []string
	arraySizes []int
	arrayBytes [][]byte

	// shared are the parameters that are per batch rather than per sprite, and
	// sharedKey is their fingerprint, values included. They enter the key by
	// value because they cannot vary within a batch at all.
	shared    []gfx.ParameterDescr
	sharedKey uint64

	params []gfx.ParameterDescr
}

func (b *spriteBatch) keyMatches(texture gfx.TextureDescr, layer m.Mat4, clip m.Rect, hasClip bool, filter gfx.FilterMode, shading *spriteShading) bool {
	if b.textureID != texture.ID() || b.layer != layer || b.clip != clip ||
		b.hasClip != hasClip || b.filter != filter ||
		b.fingerprint != shading.fingerprint || b.sharedKey != shading.sharedKey ||
		len(b.arrayNames) != len(shading.arrays) {
		return false
	}
	for i := range shading.arrays {
		if b.arrayNames[i] != shading.arrays[i].Name() || b.arraySizes[i] != shading.arrays[i].ValueSize() {
			return false
		}
	}
	return true
}

func (b *spriteBatch) add(gfxWrite *gfx.OpQueue, quad gfx.MeshDescr, texture gfx.TextureDescr, layer m.Mat4, clip m.Rect, hasClip bool, filter gfx.FilterMode, viewport m.Vec2, shading *spriteShading, t0, t1, frame, tint, misc, keyColor m.Vec4) {
	if b.active && !b.keyMatches(texture, layer, clip, hasClip, filter, shading) {
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
		b.material = shading.material
		b.fingerprint = shading.fingerprint
		b.sharedKey = shading.sharedKey
		b.shared = append(b.shared[:0], shading.shared...)
		b.startArrays(shading.arrays)
	}
	b.instances = append(b.instances, SpriteInstance{
		Transform0: t0, Transform1: t1, Frame: frame, Tint: tint, Misc: misc, KeyColor: keyColor,
	})
	for i := range shading.arrays {
		b.arrayBytes[i], _ = shading.arrays[i].AppendValue(b.arrayBytes[i])
	}
}

// startArrays reopens one buffer per per-instance parameter name, reusing the
// backing arrays across batches so a steady-state frame allocates nothing here.
func (b *spriteBatch) startArrays(arrays []gfx.ParameterDescr) {
	b.arrayNames, b.arraySizes = b.arrayNames[:0], b.arraySizes[:0]
	for len(b.arrayBytes) < len(arrays) {
		b.arrayBytes = append(b.arrayBytes, nil)
	}
	for i := range arrays {
		b.arrayNames = append(b.arrayNames, arrays[i].Name())
		b.arraySizes = append(b.arraySizes, arrays[i].ValueSize())
		b.arrayBytes[i] = b.arrayBytes[i][:0]
	}
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
	// Canvas's own parameters go first. Resolution is first-wins, so prepending
	// them is what guarantees the viewport, the transform, the clip and the
	// instance buffer against anything a caller passes; the draw's own follow,
	// and the scope's follow those.
	b.params = append(b.params[:0],
		gfx.VecParam("canvasViewport", m.Vec4{X: b.viewport.X, Y: b.viewport.Y, Z: clipEnabled}),
		gfx.MatParam("canvasLayer", b.layer),
		gfx.VecParam("canvasClip", m.Vec4{X: b.clip.X, Y: b.clip.Y, Z: b.clip.X + b.clip.Width, W: b.clip.Y + b.clip.Height}),
		gfx.BufferParam("instances", buffer),
		gfx.TextureParam(TextureSlot, b.texture),
		gfx.SamplerParam(SamplerSlot, canvasSampler(gfx.AddressClamp, gfx.AddressClamp, b.filter)),
	)
	for i := range b.arrayNames {
		b.params = append(b.params, gfx.BufferParam(b.arrayNames[i], gfx.BufferWithBytes(b.arrayBytes[i], true)))
	}
	b.params = append(b.params, b.shared...)
	gfxWrite.DrawInstanced(quad, *b.material, len(b.instances), b.params...)
	b.active = false
	b.instances = b.instances[:0]
}

// batchEntry computes one sprite/glyph instance and adds it to the batcher.
func (p *Plugin) batchEntry(gfxWrite *gfx.OpQueue, surf surface, entry atlasEntry, transform SpriteTransform, layerTransform m.Mat4, clip m.Rect, hasClip bool, shading *spriteShading, tint m.Color, keyColor m.Color) {
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
		surf.size, shading, t0, t1, uv, colorVec(tint), misc, colorVec(keyColor))
}

// trianglesShading is one triangle draw's resolved shading. Unlike a sprite
// draw's it is one parameter list rather than two: a triangle batch is
// concatenated vertices with no instance index, so the sprite path's
// per-instance arrays have nothing to hang on and every parameter a triangles
// draw names is per material. Two values really are two materials, which is why
// the key takes the parameters by value.
type trianglesShading struct {
	material    *gfx.MaterialDescr
	fingerprint uint64
	params      []gfx.ParameterDescr
	paramsKey   uint64
}

// trianglesBatch concatenates the vertices of consecutive DrawTriangles ops that
// share a vertex layout, a layer transform, a clip and their whole shading -
// material and parameter values alike - emitting them as one draw. This
// collapses the game's many small textured-quad ops (tiles, borders, walls,
// sprites) into far fewer draws.
//
// Naming a material is not itself a reason to leave the batch. Two draws
// carrying the same custom material at the same values are one draw, under the
// same rule the sprite batcher follows.
type trianglesBatch struct {
	active   bool
	layoutID int
	layout   []gfx.VertexAttr
	layer    m.Mat4
	clip     m.Rect
	hasClip  bool
	viewport m.Vec2
	vertices []byte

	material    *gfx.MaterialDescr
	fingerprint uint64
	// params are the op's own parameters, stored so the flush replays them, and
	// paramsKey is their fingerprint with values included. It comes from gfx's
	// exported helper rather than a comparison written here: a type switch in
	// canvas would silently mis-key every kind it forgot, and mis-keying merges
	// two draws that differ.
	params    []gfx.ParameterDescr
	paramsKey uint64

	scratch []gfx.ParameterDescr
}

func (b *trianglesBatch) keyMatches(layoutID int, layer m.Mat4, clip m.Rect, hasClip bool, shading *trianglesShading) bool {
	return b.layoutID == layoutID && b.layer == layer && b.clip == clip && b.hasClip == hasClip &&
		b.fingerprint == shading.fingerprint && b.paramsKey == shading.paramsKey
}

func (b *trianglesBatch) add(gfxWrite *gfx.OpQueue, viewport m.Vec2, layoutID int, layout []gfx.VertexAttr, layer m.Mat4, clip m.Rect, hasClip bool, shading *trianglesShading, vertices []byte) {
	if b.active && !b.keyMatches(layoutID, layer, clip, hasClip, shading) {
		b.flush(gfxWrite)
	}
	if !b.active {
		b.active = true
		b.layoutID = layoutID
		b.layout = layout
		b.layer = layer
		b.clip = clip
		b.hasClip = hasClip
		b.viewport = viewport
		b.vertices = b.vertices[:0]
		b.material = shading.material
		b.fingerprint = shading.fingerprint
		b.paramsKey = shading.paramsKey
		b.params = append(b.params[:0], shading.params...)
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
	b.scratch = append(b.scratch[:0],
		gfx.VecParam("canvasViewport", m.Vec4{X: b.viewport.X, Y: b.viewport.Y, Z: clipEnabled}),
		gfx.MatParam("canvasLayer", b.layer),
		gfx.VecParam("canvasClip", m.Vec4{X: b.clip.X, Y: b.clip.Y, Z: b.clip.X + b.clip.Width, W: b.clip.Y + b.clip.Height}),
	)
	b.scratch = append(b.scratch, b.params...)
	mesh := gfx.Mesh(gfx.BufferWithBytes(b.vertices, true), gfx.TopologyTriangleList, b.layout...)
	gfxWrite.Draw(mesh, *b.material, b.scratch...)
	b.active = false
	b.vertices = b.vertices[:0]
}
