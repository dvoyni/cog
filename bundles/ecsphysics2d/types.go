package ecsphysics2d

import "github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"

// Transform is a 2D affine transform, the matrix
//
//	| A C TX |
//	| B D TY |
//
// A Body's is rigid: a rotation about its Position and a translation, never a
// scale. It is physics-only and not ecsscene.Transform, which is 3D and would
// put a third axis into this package's contract.
//
// Its methods are Inverse, Mul, Point, Vec and BB; NewTransform and its
// siblings in utils.go build one.
type Transform = types.Transform

// BB is an axis-aligned bounding box: left, bottom, right, top. It is what the
// indices key on and what a broadphase rejection compares.
//
// Its methods are Intersects, Contains, ContainsVec, Merge, Expand, Centre,
// Area, Offset, SegmentQuery and IntersectsSegment; NewBB and its siblings in
// utils.go build one.
type BB = types.BB
