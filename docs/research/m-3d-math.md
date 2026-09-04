# m package 3D math coverage for scene

Research for [dvoyni/cog#7](https://github.com/dvoyni/cog/issues/7), child of the
scene map [#1](https://github.com/dvoyni/cog/issues/1). Answers: what `m` already
provides for 3D camera and transform math, which conventions it uses, whether those
match glTF and WebGPU, and exactly which functions scene still needs.

Evidence comes from reading `m/*.go` and from a throwaway probe test run against the
package (results quoted inline; the probe was not committed).

## What exists

| Function | Location | Convention notes |
| --- | --- | --- |
| `Mat2`, `Mat3`, `Mat4 [N*N]float32` | m/matrix.go:5-8 | Column-major: element (row, col) is `m[col*N+row]`. Plain arrays, no allocation, comparable with `==`. |
| `NewMat4(values ...float32)` | m/matrix.go:42 | 0 args identity, 1 arg fill, 16 args column-major. Accepts a glTF node `matrix` verbatim. |
| `Mat4.Add/Sub/MulS` | m/matrix.go:142-159 | Elementwise. |
| `Mat4.Mul(other)` | m/matrix.go:160 | Standard product `M * other`; `other` applies first to column vectors. Probe: `T.Mul(S) * (1,0,0,1) = (3,2,3,1)`. |
| `Mat4.MulVec4(v)` | m/matrix.go:170 | Column-vector transform `M * v`. No Vec3 point/direction variants. |
| `Mat4.Transpose` | m/matrix.go:173 | |
| `Mat4.Determinant` | m/matrix.go:176 | Gaussian elimination via `eliminateSquare`; allocates `[][]float32` per call. |
| `Mat4.Inverse() (Mat4, bool)` | m/matrix.go:180 | Gauss-Jordan via `inverseSquare`; allocates 4 rows + result slice per call. Correct (test `TestMatrixConstructorsAndInverse`, m/m_test.go:71). |
| `Mat3.Inverse/Transpose/Determinant` | m/matrix.go:128-140 | Same allocation caveat for `Inverse`. Usable for normal matrices once an upper-left extractor exists. |
| `Translation4(x, y, z)` | m/matrix.go:206 | Writes `m[12..14]`, i.e. the fourth column. Column-major confirmed. |
| `Scaling4(x, y, z)` | m/matrix.go:211 | Diagonal. |
| `RotationX4/Y4/Z4(angle)` | m/matrix.go:212-223 | Radians, counter-clockwise looking down the positive axis (right-handed). Probe: `RotZ(+90) * +X = (0,1,0)`, `RotY(+90) * +Z = (1,0,0)`, `RotX(+90) * +Y = (0,0,1)`. |
| `Perspective4(fovY, aspect, near, far)` | m/matrix.go:225 | Right-handed, camera looks down -Z, `m[11] = -1`. Depth maps to **0..1** (`far/(near-far)`, `far*near/(near-far)`). Probe: near -> z/w = 0, far -> z/w = 1, +Y stays up. Already WebGPU-shaped; finite far only. |
| `LookAt4(eye, center, up)` | m/matrix.go:231 | Right-handed view matrix: side = forward x up, view-space forward is -Z. Probe: eye (0,0,5) -> origin maps to (0,0,-5), +X stays +X, +Y stays +Y. Degenerate when `up` is parallel to forward (returns a zero basis, no NaN). |
| `Quat{X, Y, Z, W}` | m/quaternion.go:6 | Vector part XYZ, scalar W: same component order as glTF `rotation`. |
| `NewQuat()`, `QuatAxisAngle`, `QuatRotationX/Y/Z`, `QuatEulerXYZ` | m/quaternion.go:10-27, 110-127 | Identity is `{W: 1}`. Euler applies X, then Y, then Z (probe: equals `Rz.Mul(Ry).Mul(Rx)`). |
| `Quat.Mul(other)` | m/quaternion.go:60 | Hamilton product; `other` applies first, matching `Mat4.Mul`. Probe: `q1.Mul(q2).Rotate(v) == q1.Rotate(q2.Rotate(v))`. |
| `Quat.Rotate(v Vec3)` | m/quaternion.go:77 | `q v q^-1`; identical results to `Mat4` rotations (probe agrees to 1e-7). |
| `Quat.Mat3()`, `Quat.Mat4()` | m/quaternion.go:129-140 | Column-major rotation matrix; matches `RotationY4` in the probe. |
| `QuatFromMat3`, `QuatFromMat4` | m/quaternion.go:142-163 | Shepperd-style trace branch. Assumes a **pure rotation** upper-left 3x3: a scaled matrix gives a wrong quaternion (probe: `RotY(0.7)*S(3)` -> W 0.912 instead of 0.939). |
| `Quat.Normalize/Conjugate/Inverse/Dot/NLerp/SLerp` | m/quaternion.go:23-108 | SLerp takes the short arc and falls back to NLerp near 1. Enough for CPU-side clip sampling at bake time. |
| `Vec3` ops | m/vector.go:131-169 | Add/Sub/Mul/Div, scalar variants, Dot, Cross, Length, Normalize, Lerp. No Min/Max/Abs, no Vec3 <-> Vec4 conversion. |
| `Rect`, `Recti` | m/rect.go | 2D only; useful for viewports. No 3D box. |
| `Color`, splines, scalar helpers | m/color.go, m/spline.go, m/scalar.go | Not relevant to 3D transforms; `Lerp`, `Clamp`, `DegToRad` are reusable. |

Current consumers: `canvas` composes `Translation4(...).Mul(Scaling4(...))`
(canvas/plugin.go:208) and hands the matrix to WGSL as `canvasLayer`; the shaders
multiply `u.canvasLayer * vec4(...)` (canvas/builtin/canvas/triangles.wgsl:24), so
column-vector order is already the contract between Go and WGSL. `gfx` uploads
`Mat4` as 16 little-endian floats in array order (gfx/translate.go:399), which is
exactly WGSL `mat4x4<f32>` column-major layout; no transpose is needed. Nothing in
gfx, ui, wgpu, or the sibling modules calls `Perspective4`, `LookAt4`, or `Quat` yet,
so scene is the first 3D consumer and can still influence signatures.

## Conventions confirmed

- **Storage**: column-major, translation in `m[12], m[13], m[14]`.
- **Multiplication**: `A.Mul(B)` is `A * B`; the right operand applies first. Vectors
  are columns: `M.MulVec4(v)`. Quaternions follow the same rule.
- **Handedness**: right-handed; positive angles rotate counter-clockwise about the
  axis; `LookAt4` builds a view basis with -Z forward, +X right, +Y up.
- **Projection**: `Perspective4` writes clip depth in 0..1 with near at 0 and far at
  1, so it pairs with the wgpu backend's `DepthCompare = Less` (wgpu/gfxbackend.go:548).
- **glTF match** (spec 3.3, 3.5): glTF is right-handed, +Y up, meters, node TRS is
  `T * R * S` (scale first), `matrix` is 16 column-major floats, `rotation` is XYZW
  with W scalar, and cameras look down local -Z. Every one of those maps directly:
  `Translation4(t).Mul(rotation.Mat4()).Mul(Scaling4(s))`, `Mat4(node.Matrix)`,
  `Quat{X, Y, Z, W}` from the accessor, and `LookAt4` / `Perspective4` for cameras.
  glTF triangles are counter-clockwise front-facing and a negative
  `Mat4.Determinant()` flips winding; the wgpu backend currently sets
  `CullModeNone` (wgpu/gfxbackend.go:572), so this only matters once scene adds
  culling state to the gfx contract.
- **WebGPU match** (spec 3.3 Coordinate Systems): NDC x, y in -1..1 with +Y up, z in
  0..1, framebuffer origin top-left. `Perspective4` output is already in that space.
  Canvas flips Y itself in WGSL for its top-left 2D world; scene should not, since
  a +Y-up world with `LookAt4` lands upright without a flip.

No convention changes are needed. `m` is consistent with glTF and WebGPU as is.

## Gaps

Functions scene needs that `m` does not provide, in the package's existing style
(value receivers, radians, `(T, bool)` for fallible results, numeric suffix for the
matrix rank, no abbreviations). Types first, then constructors, then methods.

1. `func Orthographic4(left, right, bottom, top, near, far float32) Mat4` -- 0..1 depth,
   -Z forward, for glTF orthographic cameras, shadow-style passes, and UI viewports.
   glTF orthographic gives half-extents `xmag`, `ymag`; scene derives left/right from
   those.
2. `func TRS4(translation Vec3, rotation Quat, scale Vec3) Mat4` -- glTF node
   composition without three temporaries; the per-instance hot path.
3. `func (matrix Mat4) Decompose() (translation Vec3, rotation Quat, scale Vec3, ok bool)`
   -- required for glTF nodes that carry `matrix`, and for baking bone poses to TRS.
   Must divide out column lengths before `QuatFromMat3` (see the scaled-matrix note
   above) and return `ok == false` on a zero column; negative determinant flips one
   scale axis.
4. `func (matrix Mat4) TransformPoint(point Vec3) Vec3` and
   `func (matrix Mat4) TransformDirection(direction Vec3) Vec3` -- w = 1 and w = 0
   transforms without a perspective divide; used for bounding centers and light
   directions.
5. `func (matrix Mat4) Translation() Vec3` and `func (matrix Mat4) Mat3() Mat3` --
   fourth column and upper-left 3x3. The normal matrix is then
   `matrix.Mat3().Inverse()` + `Transpose()`.
6. `func (matrix Mat4) InverseAffine() (Mat4, bool)` -- closed-form inverse for
   TRS matrices (transpose the rotation part, divide by squared scales), allocation
   free. The generic `Inverse` allocates five slices per call, too costly per instance
   per frame; also worth rewriting `Mat4.Inverse` and `Determinant` as closed-form
   cofactor expansions while touching this.
7. `type Plane struct{ Normal Vec3; Distance float32 }` with
   `func (plane Plane) Normalize() Plane` and
   `func (plane Plane) SignedDistance(point Vec3) float32`.
8. `type Frustum [6]Plane` with `func FrustumFromMat4(viewProjection Mat4) Frustum` --
   Gribb/Hartmann row extraction on the column-major layout (rows are strided by 4),
   with the near plane taken from row 3 alone because depth is 0..1 rather than
   -1..1; and `func (frustum Frustum) ContainsSphere(center Vec3, radius float32) bool`
   -- the CPU cull test the map commits to.
9. `type Sphere struct{ Center Vec3; Radius float32 }` with
   `func (sphere Sphere) Transform(matrix Mat4) Sphere` -- center through
   `TransformPoint`, radius scaled by the largest column length of the upper 3x3
   (conservative under non-uniform scale).
10. `type Box3 struct{ Min, Max Vec3 }` with `func (box Box3) Sphere() Sphere`,
    `func (box Box3) Union(other Box3) Box3`, `func (box Box3) Transform(matrix Mat4) Box3`
    -- glTF `POSITION` accessors carry `min`/`max`; scene builds per-primitive spheres
    from them and unions across primitives for the per-model sphere.
11. `func (v Vec3) Min(other Vec3) Vec3`, `func (v Vec3) Max(other Vec3) Vec3`,
    `func (v Vec3) Abs() Vec3`, `func (v Vec3) Vec4(w float32) Vec4`,
    `func (v Vec4) Vec3() Vec3` -- small helpers items 9 and 10 lean on.
12. `func LookAt4` robustness: pick a fallback `up` when `forward.Cross(up)` has zero
    length instead of returning a zero basis. Behaviour change, not a new function;
    flagging it because a camera looking straight down with default up is common.

Deferred, not gaps for the spec: `Quat.LookRotation` (cameras go through `LookAt4`),
infinite-far or reverse-Z perspective (needs gfx depth-compare state first; the map's
extension-points ticket can note it), and `Project`/`Unproject` for WorldToScreen,
which the map lists under "Not yet specified" and which depends on the camera model.

Total: 11 missing functions or types plus one robustness fix. All are pure `m`
additions; none require gfx or wgpu changes.
