package m

// Blob is a run of bytes treated as static: once a value holding it is built,
// nothing writes the bytes again. It is how an engine type carrying pixels, a
// buffer's contents or a parameter's raw layout says so, and it is what lets
// such a type be an ECS Component - the ECS admits a Blob by type identity where
// it refuses every other slice, because a read handing out a copy of a static
// Blob hands out nothing a reader can use to change the Store.
//
// Nothing enforces the contract. A Blob is a []byte, and writing through one is
// an ordinary slice write that no lock names and no check sees - the ECS's
// validation mode, which catches a write through a List a read yielded, cannot
// detect a write through a Blob at all. Build the bytes, wrap them, and never
// touch them again; bytes that change belong in a new Blob.
//
// It is a []byte rather than a string because conversion is free: a []byte
// assigns to a Blob and a Blob passes where a []byte is wanted, with no copy,
// where a string would copy on the way in and again on the way out to every
// API that takes bytes.
type Blob []byte
