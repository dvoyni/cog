// The halo material: a soft outward band in one colour, and no mark at all.
//
// It is an ENTRY POINT, not an includable source, and unlike the other three it
// is not published either - canvas.HaloMaterialSet is the whole of its surface.
// keycolor.wgsl is published because a custom triangles material must reproduce
// the key-colour ramp or key every texel against black; nothing has to reproduce
// a halo.
//
// Because it never composites a mark, no band can reach anybody's ink and
// overlap is ordinary painter's order. The caller records the same marks twice -
// once on a halo layer under this material, once on the ink layer above it under
// the built-in - and the guarantee reaches exactly as far as the halo layer.
//
// It replaces BOTH entry points, so it includes spritebindings.wgsl alone rather
// than spritevertex.wgsl: vs_main has to expand the quad and the published one
// does not. It does NOT decline the bindings and re-declare SpriteInstance -
// that would be a third, untested copy of a FROZEN record.
//
// Two mechanisms, branched per instance on the frame rect:
//
//   - A sprite or a glyph carries its silhouette in the texture, so the band is
//     a ring kernel over alpha. Taps that leave s.frame are rejected outright,
//     because the sampler clamps to the 4096 page and not to this sprite: an
//     out-of-frame tap reads a NEIGHBOUR's art. A sprite's own 2px pad is no help
//     either, since the atlas extrudes its edge in alpha as well as colour.
//   - A FillRect, a Line or a StrokeRect segment is one white texel stretched
//     over a rectangle. Its frame is a degenerate point, so there is nothing to
//     sample and the texel-to-world conversion collapses to zero. Its silhouette
//     is its geometry, so the band comes from an analytic distance to the rect.
//
// The two branches are indistinguishable at the seam.

// The canvas prefix of the uniform block, then this material's own three knobs.
// An extending material hand-writes these lines and must NOT include
// uniforms.wgsl: include-once by resolved path would have declared the struct
// already, and WGSL has no way to add a member to a struct declared elsewhere.
//
// All three are per batch and arrive on the scope's MaterialSet.Params. They
// cannot be named at a draw: every valued parameter constructor sets HasValue,
// and a draw's valued parameter goes to the per-sprite storage arrays while a
// scope's is appended to the shared list. Two reaches are two scopes.
struct CanvasUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
    haloReach: f32,     // how far the band extends, in layer-local world units
    haloPlateau: f32,   // the fraction of the band that holds flat before the falloff
    haloExponent: f32,  // the shape of that falloff
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;

//#include builtin/canvas/spritebindings.wgsl
//#include builtin/canvas/clip.wgsl

// The published VertexOut is not frozen and is named nowhere in Go, so a
// material declares its own beside it under its own name. This one drops
// keyColor - the halo never reads the mark's colour - and adds two things: the
// instance index, flat, and the position within the sprite's own unit quad.
//
// The index rather than the frame rect, because every canvas binding is bound
// Vertex|Fragment, so one component buys the whole 96-byte record where the
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

const haloTau: f32 = 6.2831853;

// haloFalloff maps a distance, as a fraction of the reach, to coverage.
//
// The plateau is what makes it match hand-painted art: measured across the outer
// band of feuds' unit sprites, alpha holds near 0.82 for the first fifth of the
// band and then falls away almost exactly linearly. A plain pow(1 - t, k) from
// the ink outward does not match painted art.
fn haloFalloff(t: f32) -> f32 {
    let plateau = clamp(u.haloPlateau, 0.0, 0.99);
    let x = clamp((clamp(t, 0.0, 1.0) - plateau) / (1.0 - plateau), 0.0, 1.0);
    return pow(1.0 - x, max(u.haloExponent, 0.001));
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
    let grow = vec2<f32>(max(u.haloReach, 0.0)) / size;
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

@fragment
fn fs_main(in: HaloVertexOut) -> @location(0) vec4<f32> {
    // How many layer-local world units one screen pixel covers, taken from the
    // derivative of the interpolated local position rather than from the layer
    // matrix, because that also picks up the framebuffer scale - the one
    // quantity CanvasUniforms never carries. Taken before the clip test, so it
    // is read in uniform control flow whatever the discard does.
    let localPerPixel = max(length(dpdx(in.canvasPosition)), length(dpdy(in.canvasPosition)));

    if canvasClipped(in.canvasPosition) { discard; }

    let s = instances.data[in.index];
    let size = max(abs(s.transform0.zw), vec2<f32>(0.0001));
    let lo = min(s.frame.xy, s.frame.zw);
    let hi = max(s.frame.xy, s.frame.zw);
    let span = hi - lo;
    let reach = max(u.haloReach, 0.0001);

    var coverage = 0.0;
    if span.x <= 0.0 || span.y <= 0.0 {
        // The degenerate frame: a fill. One texel over a rectangle, so the
        // texture holds no silhouette and the geometry is the silhouette.
        let p = (in.quad - 0.5) * size;
        let outside = length(max(abs(p) - size * 0.5, vec2<f32>(0.0)));
        coverage = haloFalloff(outside / reach);
    } else {
        // A sprite or a glyph. The reach in uv, per instance, from the frozen
        // record alone: how much uv one world unit spans is exactly the frame's
        // span over the sprite's size.
        let duv = reach * span / size;

        // The tap count is derived here and published nowhere: a count right at
        // reach 6 bands at reach 30, and one right at 30 wastes taps at 6.
        //
        // The spacings are the prototype's accepted setting read back as a rule.
        // 4 rings and 12 spokes were judged right against painted art at a reach
        // of about 6 screen pixels, which works out at one ring every 1.5 px and
        // neighbouring taps about 3 px apart around the outermost ring; both
        // hold that. The caps are the ranges the prototype's own controls
        // explored, so past a reach of roughly 24 px the band softens rather
        // than costing more.
        let pixels = max(reach / max(localPerPixel, 0.0001), 1.0);
        let rings = clamp(i32(ceil(pixels / 1.5)), 1, 16);
        let spokes = clamp(i32(ceil(haloTau * pixels / 3.0)), 4, 48);

        var best = 0.0;
        for (var ring = 1; ring <= rings; ring = ring + 1) {
            let t = f32(ring) / f32(rings);
            let weight = haloFalloff(t);
            for (var spoke = 0; spoke < spokes; spoke = spoke + 1) {
                // Half a step of rotation on alternate rings, so the taps do not
                // line up into spokes of their own.
                let angle = haloTau * (f32(spoke) + 0.5 * f32(ring % 2)) / f32(spokes);
                let tap = in.uv + vec2<f32>(cos(angle), sin(angle)) * duv * t;
                // The rejection that makes this safe at any reach, and it is
                // mandatory rather than an optimisation: without it a tap past
                // the frame reads whatever the atlas packed next door, or this
                // sprite's own edge extruded into its padding.
                //
                // Component-wise rather than any(): naga's SPIR-V backend cannot
                // lower ir.ExprRelational, so a vector of bools compiles as WGSL
                // and dies at pipeline creation.
                if tap.x < lo.x || tap.y < lo.y || tap.x > hi.x || tap.y > hi.y {
                    continue;
                }
                best = max(best, textureSampleLevel(canvasTexture, canvasSampler, tap, in.atlasLayer, 0.0).a * weight);
            }
        }
        // Under the silhouette the band is at full strength, not only outside
        // it - otherwise an antialiased glyph edge blends against the backdrop
        // through the gap between the ink and the band.
        if in.uv.x >= lo.x && in.uv.y >= lo.y && in.uv.x <= hi.x && in.uv.y <= hi.y {
            best = max(best, textureSampleLevel(canvasTexture, canvasSampler, in.uv, in.atlasLayer, 0.0).a);
        }
        coverage = best;
    }

    // The band, and nothing else. The atlas was sampled for alpha only and the
    // mark's colour was never read: tint.rgb is the band's colour and tint.a its
    // peak alpha, both per sprite and free, because tint is a frozen field of
    // the instance record rather than a storage array. Fading a cluster fades
    // its halo with it.
    return vec4<f32>(in.tint.rgb, coverage * in.tint.a);
}
