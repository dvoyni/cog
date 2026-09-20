// Package types holds the concrete types the sound root aliases: the Queue
// every recorder writes, the live Voice view, the clip table that sits in front
// of libs/assets, and the seam types an Adapter speaks. They live here rather
// than in the root because sound's own code names them - a forwarder's body
// included - and because a recording queue is a concrete type a handler writes
// under a lock rather than an interface.
//
// Only packages under slots/sound may import it, which is what makes the
// exported mutators here safe: a game reaches sound through its root, where a
// read-only resource alias exposes only the reading half. What the
// implementation needs of a type's unexported state is in friends.go.
package types
