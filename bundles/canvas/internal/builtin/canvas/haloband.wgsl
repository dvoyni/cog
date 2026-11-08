// The halo's band: everything the halo material is except its fs_main.
//
// DECLARES: struct HaloProfile; @group(0) @binding(1) var<uniform> halo;
// @group(1) @binding(2) var canvasHaloDistance; struct HaloVertexOut; fn
// haloFalloff; vs_main; fn haloBand. It includes uniforms.wgsl,
// spritebindings.wgsl, clip.wgsl and tint.wgsl, so it also brings everything
// those declare.
//
// It is published for one reason: a material that has to do something to the
// band AFTER it is computed - burn it away with the rest of a frame, say -
// cannot do that from outside, because a layer's material set replaces the wider
// scope's rather than composing with it. Such a material includes this, declares
// its own uniforms at @group(0) @binding(2) and up, and writes
//
//     @fragment
//     fn fs_main(in: HaloVertexOut) -> @location(0) vec4<f32> {
//         return mine(haloBand(in));
//     }
//
// and is named over a layer as canvas.HaloMaterialSet(profile) with its Sprite
// slot replaced, which is what carries the profile to the halo uniform declared
// here. halo.wgsl is that shape with nothing done to the band.
//
// Because it never composites a mark, no band can reach anybody's ink and
// overlap is ordinary painter's order. The caller records the same marks twice -
// once on a halo layer under this material, once on the ink layer above it under
// the built-in - and the guarantee reaches exactly as far as the halo layer.
//
// It replaces the vertex stage too, so it includes spritebindings.wgsl alone
// rather than spritevertex.wgsl: vs_main has to expand the quad and the
// published one does not. It does NOT decline the bindings and re-declare
// SpriteInstance - that would be a third, untested copy of a FROZEN record.
//
// Two mechanisms, branched per instance on the frame rect:
//
//   - A sprite or a glyph carries its silhouette in the texture, and the atlas
//     carries the silhouette's distance field beside it: canvasHaloDistance, the
//     same array texel for texel, holding each texel's distance in texels to the
//     nearest ink of its own slot (alpha at least half). The band is the falloff
//     of that distance - the falloff never rises with distance, so the nearest ink
//     is the only ink that matters - read in one sample where a ring kernel over
//     alpha once searched a disc of up to 192. Outside the frame the distance is
//     the way to the frame's nearest point plus the field there, which is exact
//     where the nearest ink lies straight across the frame edge and slightly long
//     where it lies diagonally. It never samples past the frame, because the
//     sampler clamps to the 4096 page and not to this sprite: an out-of-frame
//     sample reads a NEIGHBOUR's field.
//   - A FillRect, a Line or a StrokeRect segment is one white texel stretched
//     over a rectangle. Its frame is a degenerate point, so there is nothing to
//     sample and the texel-to-world conversion collapses to zero. Its silhouette
//     is its geometry, so the band comes from an analytic distance to the rect.
//
// The two branches are indistinguishable at the seam.

// The canvas block, then this material's own three knobs as a uniform of their
// own. HaloProfile is mirrored field for field by canvas.HaloProfile, which is
// set whole as the one parameter halo.
//
// It is per batch and arrives on the scope's MaterialSet.Params. It cannot be
// named at a draw: every valued parameter constructor sets HasValue, and a
// draw's valued parameter goes to the per-sprite storage arrays while a scope's
// is the batch's. Two reaches are two scopes.
//#include builtin/canvas/uniforms.wgsl
struct HaloProfile {
    reach: f32,     // how far the band extends, in layer-local world units
    plateau: f32,   // the fraction of the band that holds flat before the falloff
    exponent: f32,  // the shape of that falloff
};
@group(0) @binding(1) var<uniform> halo: HaloProfile;

//#include builtin/canvas/spritebindings.wgsl
//#include builtin/canvas/clip.wgsl
//#include builtin/canvas/tint.wgsl

// The distance field the band is read from. It is in group 1 because it is a
// texture a draw samples, beside canvasTexture and through canvasSampler, and
// canvas binds it on every sprite draw the way it binds canvasTexture - a
// material that does not declare it leaves it unbound. Half floats, in texels
// of the atlas page, zero on ink; see distanceField in distancefield.go.
@group(1) @binding(2) var canvasHaloDistance: texture_2d_array<f32>;

// The published VertexOut is not frozen and is named nowhere in Go, so a
// material declares its own beside it under its own name. This one adds two
// things: the instance index, flat, and the position within the sprite's own
// unit quad.
//
// The index rather than the frame rect, because every canvas binding is bound
// Vertex|Fragment, so one component buys the whole 80-byte record where the
// frame alone would cost four. The quad coordinate cannot come the same way: it
// is the one thing here that varies across the fragment.
//
// That is 6 locations and 12 components, against a WebGPU floor of 16 and 60.
// Nothing in cog checks that floor - checkWebLimits counts storage buffers, bind
// groups and uniform size only - so a material widening this struct further has
// only TestTheHaloInterStageStructFitsTheWebGPUFloor between it and a pipeline
// that fails on the web and nowhere else.
struct HaloVertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) @interpolate(flat) atlasLayer: i32,
    @location(3) tint: vec4<f32>,
    @location(4) @interpolate(flat) index: u32,
    @location(5) quad: vec2<f32>,
};

// haloFalloff maps a distance, as a fraction of the reach, to coverage.
//
// The plateau is what makes it match hand-painted art: measured across the outer
// band of feuds' unit sprites, alpha holds near 0.82 for the first fifth of the
// band and then falls away almost exactly linearly. A plain pow(1 - t, k) from
// the ink outward does not match painted art.
fn haloFalloff(t: f32) -> f32 {
    let plateau = clamp(halo.plateau, 0.0, 0.99);
    let x = clamp((clamp(t, 0.0, 1.0) - plateau) / (1.0 - plateau), 0.0, 1.0);
    return pow(1.0 - x, max(halo.exponent, 0.001));
}

@vertex
fn vs_main(@location(0) quad: vec2<f32>, @builtin(instance_index) instance: u32) -> HaloVertexOut {
    let s = instances.data[instance];
    let size = max(abs(s.transform0.zw), vec2<f32>(0.0001));
    // The whole of the mechanism, in one line: grow the frozen unit quad outward
    // by the reach on every side. Expressed as a fraction of the sprite's own
    // size, so the band is the same width in world units however large the
    // sprite is.
    //
    // Nothing in canvas reads a sprite's extent - no clip intersection, no
    // batch-key field, no culling of any kind - so a quad larger than the sprite
    // is simply drawn. That is what makes this legal without a contract change.
    let grow = vec2<f32>(max(halo.reach, 0.0)) / size;
    let expanded = quad + (quad * 2.0 - 1.0) * grow;

    let origin = s.transform1.xy;
    let sine = s.transform1.z;
    let cosine = s.transform1.w;
    let scaled = (expanded - origin) * s.transform0.zw;
    let rotated = vec2<f32>(
        scaled.x * cosine - scaled.y * sine,
        scaled.x * sine + scaled.y * cosine,
    );
    let local = s.transform0.xy + rotated;
    let world = u.canvasLayer * vec4<f32>(local, 0.0, 1.0);
    let viewport = u.canvasViewport.xy;

    var out: HaloVertexOut;
    out.position = vec4<f32>(world.x * 2.0 / viewport.x - 1.0, 1.0 - world.y * 2.0 / viewport.y, 0.0, 1.0);
    out.canvasPosition = local;
    // The uv extrapolates past the frame at the same texels-per-world-unit the
    // sprite itself is drawn at, which is what lets the fragment stage measure a
    // world-unit reach in uv space without being told the scale.
    out.uv = mix(s.frame.xy, s.frame.zw, expanded);
    out.atlasLayer = i32(s.misc.x);
    out.tint = s.tint;
    out.index = instance;
    out.quad = expanded;
    return out;
}

// haloBand is the whole fragment stage: the band's colour and coverage at one
// fragment, clip test included. It discards, so it is called from fs_main and
// from nowhere else.
fn haloBand(in: HaloVertexOut) -> vec4<f32> {
    if canvasClipped(in.canvasPosition) { discard; }

    let s = instances.data[in.index];
    let size = max(abs(s.transform0.zw), vec2<f32>(0.0001));
    let lo = min(s.frame.xy, s.frame.zw);
    let hi = max(s.frame.xy, s.frame.zw);
    let span = hi - lo;
    let reach = max(halo.reach, 0.0001);

    var coverage = 0.0;
    if span.x <= 0.0 || span.y <= 0.0 {
        // The degenerate frame: a fill. One texel over a rectangle, so the
        // texture holds no silhouette and the geometry is the silhouette.
        let p = (in.quad - 0.5) * size;
        let outside = length(max(abs(p) - size * 0.5, vec2<f32>(0.0)));
        coverage = haloFalloff(outside / reach);
    } else {
        // Under the ink the band is already at full strength and nothing can
        // beat it. A fragment inside the frame whose own texel is opaque has its
        // answer before the field is read, and the sample is needed below
        // anyway.
        var own = 0.0;
        if all(in.uv >= lo) && all(in.uv <= hi) {
            own = textureSampleLevel(canvasTexture, canvasSampler, in.uv, in.atlasLayer, 0.0).a;
            if own >= 1.0 {
                return haloColor(in.tint, 1.0);
            }
        }

        // World units per uv, per axis, from the frozen record alone: the
        // sprite's size over the frame's span. The uv extrapolates past the
        // frame at the same rate, so a fragment outside it is this far from the
        // frame's nearest point.
        let localPerUV = size / span;
        let nearest = clamp(in.uv, lo, hi);
        let beyond = length((in.uv - nearest) * localPerUV);

        // The field is in texels, isotropic in the atlas. A sprite drawn at
        // different scales on its two axes has no single texel size in world
        // units, and the geometric mean is the one that is exact for every
        // uniform scale.
        let atlas = vec2<f32>(textureDimensions(canvasHaloDistance).xy);
        let localPerTexel = localPerUV / atlas;
        let texels = textureSampleLevel(canvasHaloDistance, canvasSampler, nearest, in.atlasLayer, 0.0).r;
        let distance = texels * sqrt(localPerTexel.x * localPerTexel.y) + beyond;

        // Under the silhouette the band is at full strength, not only outside
        // it - otherwise an antialiased glyph edge blends against the backdrop
        // through the gap between the ink and the band. The field is zero on
        // ink, and own covers a fringe below the threshold.
        coverage = max(haloFalloff(distance / reach), own);
    }

    // The band, and nothing else. The atlas was sampled for alpha only and the
    // mark's colour was never read: the band is white art under tint, and tint.a
    // its peak alpha, both per sprite and free, because tint is a frozen field of
    // the instance record rather than a storage array. Fading a cluster fades
    // its halo with it.
    return haloColor(in.tint, coverage);
}

// haloColor is the band at coverage: the colour white art would take under
// tint, so a band and a mark tinted alike are the same colour, and tint.a as
// its peak alpha.
fn haloColor(tint: vec4<f32>, coverage: f32) -> vec4<f32> {
    return canvasTint(vec4<f32>(1.0), vec4<f32>(tint.rgb, coverage * tint.a));
}
