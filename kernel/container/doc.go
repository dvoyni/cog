// Package container holds the building blocks that the kernel and the
// packages built on it share.
//
//   - Pool is a growing store of slots addressed by index. Queue is a FIFO of
//     any value, such as those indexes. Both are safe from any goroutine.
//   - Array is a growing sequence of values, and Bits a growing set of
//     indexes. Neither is synchronized: their owner keeps them apart.
//   - IsPointerFree checks, once at registration, that a type can live in
//     them without the GC scanning it.
//
// Every one of them grows in fixed-size chunks that never move. Growing
// copies nothing, a pointer to an item stays valid while the item is there,
// and once a container has reached its working size it allocates nothing.
//
// Pool slots are addressed by uint32 index, never by pointer, so whatever
// refers to them holds no pointers and the GC need not scan it. Index 0 is
// never handed out and means none.
package container
