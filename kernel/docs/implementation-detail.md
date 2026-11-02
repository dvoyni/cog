# kernel v2: implementation details

Temporary notes on decisions made while designing the implementation, kept so
they are not forgotten. Delete this file once the implementation is finished
and settled.

## Successor lists are edge lists, not Queues

Every job keeps the list of what runs after it: dependent jobs, and the
`Signal`s armed by `Notify`. The list must be closable, so that a dependent
added after the job has finished learns that at once instead of waiting
forever.

- **Layout.** Each of a job slot's two lists is one atomic `uint64`: the
  slot's generation in the high 32 bits, the list's head edge in the low 32.
  Each edge holds its target and the index of the next edge. Dependent jobs
  and `Signal`s are two lists in two edge pools, because a signal edge holds a
  pointer: keeping it apart leaves the job slots and successor edges
  pointer-free, so the GC scans only the signal edges.
- **Add** is one compare-and-swap that pushes the edge, and succeeds only
  while the word's generation is the handle's and the head is not the closed
  marker. A finished job refuses, and the dependent does not count it; a slot
  freed and reused meanwhile has a new generation, so a stale handle refuses
  too. No mutex.
- **Finish** swaps the closed marker into both words and walks what it took.
  **Free** stores the next generation, still closed, so every old handle reads
  as finished.
- **Why not a mutex per job.** It works, and is uncontended in the normal
  case, but it guards one invariant (still this job, still running, then
  link) across several fields. Packing generation and head into one word makes
  that a single compare-and-swap, and contention is per job and rare, so the
  cost that made shared lock-free stacks lose to a mutex does not apply.
- **Why not `Queue[uint32]` with `Close`.** It works, but every job slot would
  then cost about 64 bytes for the queue, allocate a ring the first time it
  gets a successor, and hold a slice, so the GC would scan the job pool. The
  edge list is about 16 bytes per job, holds no pointers and allocates nothing
  beyond the edge pool's own growth.
