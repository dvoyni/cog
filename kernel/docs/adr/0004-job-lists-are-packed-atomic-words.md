# A job's successor and signal lists are packed atomic words, not a mutex per job

Each list is one `uint64` holding the slot's generation and the head edge, so linking is one compare-and-swap that fails once the job has finished or the slot was reused. A mutex per job would guard the same invariant across several fields; one word makes it one compare-and-swap, contention is per job and rare, and the job slot stays pointer-free so the GC never scans job storage.
