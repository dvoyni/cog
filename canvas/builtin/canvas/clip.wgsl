// The layer clip test, as one function.
//
// DECLARES: fn canvasClipped. No binding, no struct - only a read of u, which
// module-scope order independence permits, so this is includable by extending
// and non-extending materials alike. That is the whole reason it is its own
// source rather than a helper inside uniforms.wgsl: the material most likely to
// omit the test is the one that extends the block, and that one can never
// include uniforms.wgsl.
//
// WARNING: calling it is offered, not required, and canvas cannot enforce a call
// inside a body it does not own. SetClip and RemoveClip are implemented entirely
// as this shader test - there is no scissor rect anywhere in gfx - so a
// hand-written fs_main that omits it draws outside the clip rectangle with no
// error anywhere. The three built-in entry points call it, and are the worked
// example.
//
// It returns bool rather than discarding, so the discard stays visible at the
// call site and a material that clips by writing transparent black can do that
// instead:
//
//     if canvasClipped(in.canvasPosition) { discard; }
fn canvasClipped(canvasPosition: vec2<f32>) -> bool {
    return u.canvasViewport.z > 0.5 && (
        canvasPosition.x < u.canvasClip.x || canvasPosition.y < u.canvasClip.y ||
        canvasPosition.x > u.canvasClip.z || canvasPosition.y > u.canvasClip.w
    );
}
