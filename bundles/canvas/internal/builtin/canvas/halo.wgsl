// The halo material: a soft outward band in one colour, and no mark at all.
//
// It is an ENTRY POINT, not an includable source, and it is not published:
// canvas.HaloMaterialSet is the way to it. Its whole body is its include and
// fs_main, which is the shape every material built on the band has - the band
// itself, and why it is its own source, is in haloband.wgsl.
//#include builtin/canvas/haloband.wgsl

@fragment
fn fs_main(in: HaloVertexOut) -> @location(0) vec4<f32> {
    return haloBand(in);
}
