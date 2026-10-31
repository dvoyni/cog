# Scheduler lock load test

This measures what the kernel's resource locks cost a system for the lock
mechanism alone, separate from the time the system waits for a conflicting
holder to finish its work. It was written for the v2 scheduler rework, and
records two schedulers on the same workload and the same machine:

- **Baseline** is the coordinator scheduler at `main` `d13b6e6`, with the test
  harness added on the `v2` branch.
- **Rework** is the track scheduler on `v2`, which replaced it.

## Summary

| | baseline | rework, blocking | rework, pool |
|---|---|---|---|
| frame wall time | 10.12 ms | 5.36 ms | **4.81 ms** |
| zero-overhead bound for the same order | — | 5.11 ms | 4.53 ms |
| lock mechanism, mean per system run | 3.67 ms | 110 µs | **43 µs** |
| lock mechanism, sum per frame | 1100 ms | 33 ms | **13 ms** |

These are means of three runs, alternating baseline and rework so that machine
noise affects both equally. Each run varied by about ±2%.

- **The mechanism costs about 85× less per system run.** Most of that comes from
  removing the coordinator: blocking mode keeps the old one-goroutine-per-system
  shape and changes only the lock protocol.
- **A frame is 2.1× faster**, and now finishes within 6% of the bound it could
  reach if locking cost nothing. What is left of the frame time is caused by the
  lock sets and the order systems take each resource in, not by the mechanism.
  Further gains would have to come from choosing a better order.
- **A single dispatch is 24× faster.** `BenchmarkExecuteCommand` drops from
  1460 ns to 61 ns (see [Micro-benchmarks](#micro-benchmarks)).

## Environment

```
go version go1.27.1 windows/amd64
AMD Ryzen 9 7950X3D 16-Core Processor, GOMAXPROCS=32
```

## How to run

```
go test -tags kernel_schedtrace -run TestSchedulerLockOverhead -v ./kernel
```

The test is `kernel/schedulerload_test.go`. It builds only under the
`kernel_schedtrace` tag, which also enables the scheduler's trace hooks in
`kernel/schedtrace_on.go`. Without the tag, `kernel/schedtrace_off.go` turns
every hook into an empty function, so the scheduler runs exactly as it does
untraced.

The baseline harness differs from the rework's, because it traced the
coordinator's messages. It exists only with the baseline code.

## Workload

| | |
|---|---|
| Components | 200 resources |
| Systems | 300. Each writes 1–2 components and reads 1–5 others, all picked at random with a fixed seed (`PCG(1, 2)`). |
| Work | Each system spins for a fixed time drawn log-uniform from 30 ns to 1 ms, giving a median of 4.6 µs and a mean of 90 µs. That is about 27 ms of work per frame. |
| Runs | 20 warmup frames, then 200 measured frames (60,000 system runs). |

The rework is measured two ways:

- **Blocking.** All 300 systems start on their own goroutines at the same
  moment, and each calls `scheduler.execute`. This is how `runPublication`
  started a phase's subscribers before the rework, and it is the shape the
  baseline was measured in.
- **Pool.** The frame is submitted as one batch with `scheduler.submitBatch`,
  and pool workers run it. This is how `runPublication` runs a phase now.

## What is counted as mechanism

Every timestamp is a TSC read (`runtime.cputicks`, about 14 ns), calibrated
against `time.Now` over 500 ms. On Windows, `time.Now` and `runtime.nanotime`
only tick every 0.5 ms, which is too coarse to measure the protocol at all.

The scheduler records three things for each job: when it was placed on its
tracks, when it was granted, and when its locks were released. It also records
the cause of each grant: the job whose finish granted it, or none if it was
granted on submission. Only a finish can unblock a job, so the cause identifies
the last holder the job was waiting on. (The baseline recorded the same three
things as coordinator messages: acquire received, grant, release received.)

A system's **ready** time is when an ideal lock would have granted it. That is
either the moment it was submitted, or the moment the blocking holder finished
its work, whichever is later.

- **Mechanism** is the sum of these four steps:
  1. submit, from handing the system to the scheduler until it is on its tracks
  2. grant delay, from ready until granted
  3. wake, from the grant until the system is running
  4. release, from the end of the work until its locks are released
- **Resource wait** is the time from submission until ready. It is the cost of
  the lock sets themselves, not of the mechanism, and is reported only for
  context.

The **zero-overhead bound** replays each frame with the protocol costing nothing
and unlimited cores. Each system starts the moment the systems it waits on have
finished, in the order the frame actually submitted them, using the work each
system actually did. A frame can't finish faster than that without a different
order.

## Rework results

The full tables are from one run.

### Pool

Frame wall time **4.79 ms**, against a zero-overhead bound of 4.53 ms. Of the
60,000 runs, 4,000 were granted on submission and 56,000 by a finish.

| per system run | mean | p50 | p90 | p99 | max | sum per frame |
|---|---|---|---|---|---|---|
| submit (handed over → on its tracks) | 29.8 µs | 28.6 µs | 51.0 µs | 98.2 µs | 236.7 µs | 8.95 ms |
| grant, free (on its tracks → granted) | 126 ns | 110 ns | 240 ns | 400 ns | 890 ns | 2.5 µs |
| grant, blocked (holder done → granted) | 450 ns | 320 ns | 670 ns | 1.6 µs | 117.5 µs | 126.0 µs |
| wake (granted → running) | 11.4 µs | 2.3 µs | 22.8 µs | 102.5 µs | 270.6 µs | 3.42 ms |
| release (work done → locks released) | 202 ns | 60 ns | 220 ns | 1.8 µs | 143.1 µs | 60.5 µs |
| **lock mechanism, total** | **41.9 µs** | 37.5 µs | 72.1 µs | 137.6 µs | 304.5 µs | **12.56 ms** |
| resource wait (not mechanism) | 1.83 ms | 1.48 ms | 3.79 ms | 4.45 ms | 4.97 ms | 550.24 ms |
| work | 89.8 µs | 4.6 µs | 307.7 µs | 886.6 µs | 979.0 µs | 26.94 ms |

### Blocking

Frame wall time **5.40 ms**, against a zero-overhead bound of about 5.1 ms. Of
the 60,000 runs, 6,181 were granted on submission and 53,819 by a finish.

| per system run | mean | p50 | p90 | p99 | max | sum per frame |
|---|---|---|---|---|---|---|
| submit (handed over → on its tracks) | 96.5 µs | 5.4 µs | 241.4 µs | 431.5 µs | 1.22 ms | 28.96 ms |
| grant, free (on its tracks → granted) | 351 ns | 310 ns | 590 ns | 1.0 µs | 3.1 µs | 10.9 µs |
| grant, blocked (holder done → granted) | 4.9 µs | 410 ns | 870 ns | 187.2 µs | 1.09 ms | 1.33 ms |
| wake (granted → running) | 5.5 µs | 2.8 µs | 12.6 µs | 31.7 µs | 973.9 µs | 1.66 ms |
| release (work done → locks released) | 4.8 µs | 150 ns | 520 ns | 188.5 µs | 1.09 ms | 1.44 ms |
| **lock mechanism, total** | **111.3 µs** | 40.9 µs | 251.3 µs | 476.0 µs | 1.30 ms | **33.40 ms** |
| resource wait (not mechanism) | 1.82 ms | 1.70 ms | 3.70 ms | 4.98 ms | 7.05 ms | 544.90 ms |
| work | 89.8 µs | 4.6 µs | 307.7 µs | 886.6 µs | 957.2 µs | 26.94 ms |

### Reading the rework numbers

- **Granting is now almost free.** A grant takes 0.1–0.5 µs whether or not the
  job had to wait, against 7.7 µs and 185 µs in the baseline.
- **Resource wait doubled, and that is expected.** The baseline coordinator was
  so slow that it spread systems' arrivals out over the frame, and a system
  that arrives late rarely finds its resources taken. Now all 300 arrive within
  microseconds and queue on real conflicts. The zero-overhead bound shows this
  wait comes from the lock sets.
- **In pool mode, most of what remains is batch submission and wake-up.**
  - The submit row is a job's position in the 300-job batch: the batch takes
    about 60 µs to place under one lock, so the mean job waits half of that.
  - The wake row is the time from grant to running when the job's finisher
    could not run it inline, so a worker had to be signalled or started.
- **In blocking mode, the submit row is 300 goroutines contending for one mutex
  at once.** That is the shape the pool replaces.

## Micro-benchmarks

`go test -run XXX -bench . -benchmem ./kernel`, three runs each, medians shown.

| benchmark | baseline | rework | change |
|---|---|---|---|
| `ExecuteCommand` | 1458 ns, 0 allocs | 60.7 ns, 0 allocs | 24× faster |
| `UsesNestedDispatch` | 1479 ns, 0 allocs | 83.2 ns, 0 allocs | 18× faster |
| `PublishEventSingleSubscriber` | 2275 ns, 5 allocs | 539.8 ns, 3 allocs | 4.2× faster |
| `PublishEventThreeSubscribers` | 1804 ns, 9 allocs | 648.0 ns, 3 allocs | 2.8× faster |
| `ExecuteExclusiveCommand` | 1536 ns, 0 allocs | 62.6 ns, 0 allocs | 25× faster |

A synchronous command no longer makes a channel round trip to another
goroutine. It takes the scheduler's mutex once to submit and once to finish,
and runs on its own goroutine. A publication no longer starts a goroutine per
subscriber. It submits the batch and returns, and an idle worker picks it up.

## Baseline results

Three runs gave mechanism means of 3.79, 3.86 and 3.72 ms per system run, and
frame wall times of 10.27, 10.45 and 10.16 ms. The full table below is from the
first run. The three runs alternating with the rework gave 3.67, 3.69 and
3.64 ms, and 10.21, 10.15 and 10.00 ms.

| per system run | mean | p50 | p90 | p99 | max | sum per frame |
|---|---|---|---|---|---|---|
| acquire handoff (send → coordinator receives) | 3.45 ms | 3.00 ms | 7.32 ms | 9.79 ms | 16.68 ms | 1033.84 ms |
| grant, free (coordinator receives → grant) | 7.7 µs | 6.2 µs | 14.1 µs | 23.6 µs | 747.0 µs | 356.3 µs |
| grant, blocked (holder done → grant) | 185.3 µs | 130.2 µs | 396.7 µs | 1.06 ms | 3.84 ms | 47.06 ms |
| wake (grant → worker running) | 9.6 µs | 6.3 µs | 15.4 µs | 49.2 µs | 3.79 ms | 2.89 ms |
| release handoff (work done → coordinator receives) | 177.7 µs | 122.7 µs | 391.3 µs | 1.03 ms | 3.85 ms | 53.30 ms |
| **lock mechanism, total** | **3.79 ms** | 3.45 ms | 7.66 ms | 10.04 ms | 16.71 ms | **1137.45 ms** |
| resource wait (not mechanism) | 877.8 µs | 656.7 µs | 2.07 ms | 3.92 ms | 8.95 ms | 263.35 ms |
| work | 89.8 µs | 4.6 µs | 307.7 µs | 886.6 µs | 955.8 µs | 26.95 ms |

- Of the 60,000 runs, 9,202 were granted as soon as their acquire arrived and
  50,798 were granted when a release unblocked them.
- Frame wall time averaged **10.27 ms**.

The coordinator was a single goroutine. The counts below include one barrier
request pair per frame.

| | |
|---|---|
| time per acquire message | 13.6 µs (60,200 messages) |
| time per release message | 14.2 µs (60,201 messages) |
| busy per frame | 8.38 ms of 10.27 ms wall (**81.6% utilised**) |
| pending requests scanned per dispatch | 26.7 mean |

### Baseline variations

These are single runs, changing one setting each.

| variation | frame wall | mechanism mean | time per message (acquire / release) | pending scanned per dispatch |
|---|---|---|---|---|
| baseline (GOMAXPROCS=32, 30 ns–1 ms) | 10.27 ms | 3.79 ms | 13.6 / 14.2 µs | 26.7 |
| almost no work (30–31 ns) | 2.66 ms | 1.14 ms | 3.6 / 3.7 µs | 7.5 |
| GOMAXPROCS=8 | 10.29 ms | 3.82 ms | 14.1 / 14.5 µs | 30.6 |
| GOMAXPROCS=4 | 14.42 ms | 5.02 ms | 20.2 / 21.0 µs | 46.1 |

### Why the baseline was slow

The single coordinator was saturated, and its per-message cost grew with the
length of the pending queue.

- Every frame began with 300 acquires arriving at once. At about 14 µs each
  they queued for up to 4 ms, which is the acquire handoff row. That row alone
  was 91% of the mechanism total.
- Almost all of the coordinator's 14 µs per message was spent in `dispatch`,
  which rescanned every pending request on every message.
- A CPU profile confirmed the busy time was real CPU: about 1.6 s of CPU over a
  2.8 s run. `dispatch` rebuilt its `blockedResources` map from scratch on every
  message (13% of all CPU in the run), looked up `reflect.Type` keys in maps
  (about 5%), and paid for a random iteration start on every `range` over a
  request's maps.

## What the rework changed

- **Lock sets are compiled at finalization.** Every lock key, resources and
  `Exclusive` keys alike, gets a dense track index. Each handler gets a sorted
  index slice (`lockSet`). The scheduler never hashes a `reflect.Type`.
- **There is no coordinator.** Each resource keeps a track: its last writer, and
  the readers since. Submitting a job puts it on the tail of each of its tracks
  and counts the unfinished jobs it waits for. Finishing a job decrements its
  successors' counts and grants any that reach zero. Both run under one mutex,
  and their cost depends on the job's own lock set, not on how many jobs are
  pending.
- **Jobs are named by ID, not pointer.** An ID is a slot index plus that slot's
  generation, and a slot is reused only after its job has finished. An ID a
  track still holds therefore reads as "finished" rather than naming whoever
  took the slot next.
- **Publications run on a worker pool.** A publication submits its subscribers
  as one batch, in dependency order, under one lock. `Before`/`After` edges are
  ordered edges between jobs, and a failure skips whatever is ordered after it.
  A worker that finishes a job runs the next job it made ready itself. The pool
  starts a worker whenever a job is ready and none is idle, so a ready job never
  waits behind a worker blocked in plugin code. It keeps up to `GOMAXPROCS` idle
  workers parked.
- **Each resource is granted in submission order**, which makes the order
  deterministic within a publication. Before, two conflicting subscribers took a
  resource in whatever order their goroutines reached the coordinator.
