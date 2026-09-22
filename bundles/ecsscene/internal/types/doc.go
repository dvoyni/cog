// Package types declares ecsscene's own camera, layer and pass vocabulary,
// which the root aliases: CameraID, ProjectionKind, PassTag, Pass and
// LayerMask, with Layer behind the root's forwarder. Each keeps scene's name,
// shape and zero-value meaning, and names nothing of scene.
//
// They are declared here rather than in the root because the root's exported
// functions forward into its own internal/types, and Layer returns a
// LayerMask. Nothing declared here imports the root.
package types
