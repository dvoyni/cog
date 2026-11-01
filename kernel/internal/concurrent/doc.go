// Package concurrent holds the kernel's shared building blocks: Pool, a
// growing store of slots addressed by index, and Queue, a FIFO of any
// value, such as those indexes. Both are safe from any goroutine.
//
// Pool slots are addressed by uint32 index, never by pointer, so whatever
// refers to them holds no pointers and the GC need not scan it. Index 0 is
// never handed out and means none.
package concurrent
