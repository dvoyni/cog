# kernel v2: implementation details

Temporary notes on decisions made while designing the implementation, kept so
they are not forgotten. Delete this file once the implementation is finished
and settled.

## Successor lists are edge lists, not Queues

Every job keeps the list of what runs after it: dependent jobs, and the
`Signal`s armed by `Notify`. The list must be closable, so that a dependent
added after the job has finished learns that at once instead of waiting
forever.

- **Layout.** The job slot holds the index of its first edge in the kernel's
  edge `Pool`, a small mutex, and a closed flag. Each edge holds its target (a
  job, or a `Signal`) and the index of the next edge.
- **Add** takes the job's mutex. If the list is closed, the predecessor has
  already finished and the dependent does not count it. Otherwise the edge is
  pushed at the head.
- **Finish** takes the mutex, sets closed, takes the head, and walks the edges
  after unlocking. The edges go back to the pool with `PutMany`.
- **Why not `Queue[uint32]` with `Close`.** It works, but every job slot would
  then cost about 64 bytes for the queue, allocate a ring the first time it
  gets a successor, and hold a slice, so the GC would scan the job pool. The
  edge list is about 16 bytes per job, holds no pointers and allocates nothing
  beyond the edge pool's own growth.
