package ecsscene

// Manifest is the read-only resource the recording System resolves names
// through: which glTF file a ModelHash names, and which clip inside a file a
// ClipHash names.
//
// A name table belongs to the side that reads a name, not to the side that
// writes one — one System declares it, rather than every System that ever
// assigns a model to an Entity — and it has no lock of its own and wants none.
// It is an ordinary value behind an ordinary resource, so its lock set is this
// resource's, declared with ecs.Read[*Manifest] and taken by the kernel at
// registration like any other. A mutex inside it would be the global lock the
// hash scheme exists to avoid, paid on every draw.
//
// It is filled from Config during Registration and never again, which is why
// nothing here mutates it: a mutator would be reachable through a read handle,
// and a read-locked System holding a mutator is a race the kernel cannot see.
type Manifest = manifest
