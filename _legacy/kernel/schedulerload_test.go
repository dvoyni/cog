//go:build kernel_schedtrace

package kernel

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The lock load test: a frame of loadSystems systems over loadComponents
// components, each holding its lock set for a fixed spin, run two ways.
//
//	go test -tags kernel_schedtrace -run TestSchedulerLockOverhead -v ./kernel
//
// blocking starts every system on its own goroutine at the same moment and has
// each call execute, which is how runPublication ran a phase before the pool:
// it measures the lock protocol alone. pool submits the frame as one batch the
// way runPublication does now, and workers run it: it measures what a
// publication costs end to end.
//
// Both report what the lock protocol itself costs a system, and keep that apart
// from the time a system spends waiting for a holder of a conflicting lock to
// finish its work, which is the cost of the lock set and not of the mechanism.
const (
	loadComponents = 200
	loadSystems    = 300
	loadFrames     = 200
	loadWarmup     = 20
	loadMinWork    = 30 * time.Nanosecond
	loadMaxWork    = time.Millisecond
)

// loadRun is one system's execution in one frame. The scheduler stamps trace;
// the rest are stamped around it, in ticks.
type loadRun struct {
	started int64 // the system was handed to the scheduler
	woke    int64 // the system began running
	ended   int64 // the system's work was done
	trace   jobTrace
}

// loadTask is one system: its lock set, its work, and the run it is recording.
type loadTask struct {
	lockSet lockSet
	work    int64 // ticks
	current *loadRun
}

func (t *loadTask) locks() *lockSet     { return &t.lockSet }
func (t *loadTask) jobTrace() *jobTrace { return &t.current.trace }

func (t *loadTask) run(any) error {
	t.current.woke = cputicks()
	for end := t.current.woke + t.work; cputicks() < end; {
	}
	t.current.ended = cputicks()
	return nil
}

// loadFrame owns one pooled frame's jobs and says when the last has finished.
type loadFrame struct {
	remaining atomic.Int32
	done      chan struct{}
}

func (f *loadFrame) jobDone(error) {
	if f.remaining.Add(-1) == 0 {
		close(f.done)
	}
}

func TestSchedulerLockOverhead(t *testing.T) {
	ticksPerNs := calibrateTicks()
	systems := loadWorkload(ticksPerNs)
	for _, mode := range []struct {
		name  string
		frame func(*scheduler, []*loadTask, []loadRun)
	}{
		{"blocking", blockingFrame},
		{"pool", poolFrame},
	} {
		t.Run(mode.name, func(t *testing.T) {
			s := newScheduler()
			defer s.shutdown()
			for range loadWarmup {
				mode.frame(s, systems, make([]loadRun, len(systems)))
			}
			schedulerTrace.submits.Store(0)
			schedulerTrace.finishes.Store(0)

			var runs []loadRun
			var wall int64
			for range loadFrames {
				frame := make([]loadRun, len(systems))
				began := cputicks()
				mode.frame(s, systems, frame)
				wall += cputicks() - began
				runs = append(runs, frame...)
			}
			t.Log("\n" + loadReport(mode.name, systems, runs, wall, ticksPerNs))
		})
	}
}

// loadWorkload builds the systems. The seed is fixed, so every run and every
// mode sees the same lock sets and the same work.
func loadWorkload(ticksPerNs float64) []*loadTask {
	rng := rand.New(rand.NewPCG(1, 2))
	systems := make([]*loadTask, loadSystems)
	for i := range systems {
		picked := rng.Perm(loadComponents)
		writes, reads := 1+rng.IntN(2), 1+rng.IntN(5)
		system := &loadTask{}
		for _, c := range picked[:writes] {
			system.lockSet.write = append(system.lockSet.write, int32(c))
		}
		for _, c := range picked[writes : writes+reads] {
			system.lockSet.read = append(system.lockSet.read, int32(c))
		}
		slices.Sort(system.lockSet.read)
		slices.Sort(system.lockSet.write)
		system.lockSet.span = loadComponents
		// Log-uniform: most systems are short, a few are long, as in a game.
		logMin, logMax := math.Log(float64(loadMinWork)), math.Log(float64(loadMaxWork))
		system.work = int64(math.Exp(logMin+rng.Float64()*(logMax-logMin)) * ticksPerNs)
		systems[i] = system
	}
	return systems
}

func blockingFrame(s *scheduler, systems []*loadTask, runs []loadRun) {
	var wg sync.WaitGroup
	for i, system := range systems {
		system.current = &runs[i]
		wg.Go(func() {
			runs[i].started = cputicks()
			_ = s.execute(system, nil, system.locks())
		})
	}
	wg.Wait()
}

func poolFrame(s *scheduler, systems []*loadTask, runs []loadRun) {
	jobs := make([]batchJob, len(systems))
	for i, system := range systems {
		system.current = &runs[i]
		jobs[i] = batchJob{task: system, locks: system.locks()}
	}
	frame := &loadFrame{done: make(chan struct{})}
	frame.remaining.Store(int32(len(jobs)))
	started := cputicks()
	for i := range runs {
		runs[i].started = started
	}
	_ = s.submitBatch(jobs, nil, frame)
	<-frame.done
}

func loadReport(mode string, systems []*loadTask, runs []loadRun, wall int64, ticksPerNs float64) string {
	byID := make(map[jobID]*loadRun, len(runs))
	for i := range runs {
		byID[runs[i].trace.id] = &runs[i]
	}

	var submit, grantFree, grantBlocked, wake, release, mechanism, waited, worked []int64
	var blocked int
	for i := range runs {
		run := &runs[i]
		trace := &run.trace
		submit = append(submit, trace.submittedAt-run.started)
		// ready is when an ideal lock would have granted: on submission, or the
		// moment the holder it was last waiting on finished its work.
		ready := trace.submittedAt
		if trace.cause != 0 {
			blocked++
			ready = max(ready, byID[trace.cause].ended)
			grantBlocked = append(grantBlocked, trace.grantedAt-ready)
		} else {
			grantFree = append(grantFree, trace.grantedAt-trace.submittedAt)
		}
		wake = append(wake, run.woke-trace.grantedAt)
		release = append(release, trace.releasedAt-run.ended)
		waited = append(waited, ready-trace.submittedAt)
		worked = append(worked, run.ended-run.woke)
		mechanism = append(mechanism,
			(trace.submittedAt-run.started)+(trace.grantedAt-ready)+(run.woke-trace.grantedAt)+(trace.releasedAt-run.ended))
	}

	ns := func(ticks int64) string { return formatNs(float64(ticks) / ticksPerNs) }
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d components, %d systems, %d frames (after %d warmup)\n",
		mode, loadComponents, loadSystems, loadFrames, loadWarmup)
	var bound int64
	for frame := range loadFrames {
		bound += criticalPath(systems, runs[frame*len(systems):(frame+1)*len(systems)])
	}
	fmt.Fprintf(&out, "frame wall time: %s mean; %s with no overhead and unlimited cores\n",
		ns(wall/loadFrames), ns(bound/loadFrames))
	fmt.Fprintf(&out, "runs: %d, granted on submission %d, granted by a finish %d\n\n",
		len(runs), len(runs)-blocked, blocked)

	fmt.Fprintf(&out, "%-42s %10s %10s %10s %10s %10s %12s\n", "per system run", "mean", "p50", "p90", "p99", "max", "sum/frame")
	row := func(name string, values []int64) {
		if len(values) == 0 {
			return
		}
		sorted := slices.Clone(values)
		slices.Sort(sorted)
		var sum int64
		for _, v := range sorted {
			sum += v
		}
		at := func(q float64) int64 { return sorted[min(len(sorted)-1, int(q*float64(len(sorted))))] }
		fmt.Fprintf(&out, "%-42s %10s %10s %10s %10s %10s %12s\n", name,
			ns(sum/int64(len(sorted))), ns(at(0.5)), ns(at(0.9)), ns(at(0.99)), ns(sorted[len(sorted)-1]), ns(sum/loadFrames))
	}
	row("submit (handed over -> on its tracks)", submit)
	row("grant, free (on its tracks -> granted)", grantFree)
	row("grant, blocked (holder done -> granted)", grantBlocked)
	row("wake (granted -> running)", wake)
	row("release (work done -> locks released)", release)
	row("= lock mechanism overhead", mechanism)
	out.WriteString("\n")
	row("resource wait (not mechanism)", waited)
	row("work", worked)
	fmt.Fprintf(&out, "\nscheduler: %d submits, %d finishes; each stamp costs ~%s\n",
		schedulerTrace.submits.Load(), schedulerTrace.finishes.Load(), ns(stampCost()))
	return out.String()
}

// criticalPath is how long one frame would take if the lock protocol cost
// nothing and every system had a core: each system starts the moment the
// systems it waits on have finished, under the same rule the scheduler grants
// by, in the order the frame actually submitted them, with the work each
// actually did.
func criticalPath(systems []*loadTask, runs []loadRun) int64 {
	order := make([]int, len(systems))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return int(runs[a].trace.submittedAt - runs[b].trace.submittedAt) })
	// tail is a track's state in finish times: when its writer finishes, and
	// when the last reader since that writer finishes.
	type tail struct{ writer, readers int64 }
	tracks := make([]tail, loadComponents)
	var longest int64
	for _, i := range order {
		var start int64
		for _, index := range systems[i].lockSet.read {
			start = max(start, tracks[index].writer)
		}
		for _, index := range systems[i].lockSet.write {
			start = max(start, tracks[index].writer, tracks[index].readers)
		}
		end := start + (runs[i].ended - runs[i].woke)
		for _, index := range systems[i].lockSet.read {
			tracks[index].readers = max(tracks[index].readers, end)
		}
		for _, index := range systems[i].lockSet.write {
			tracks[index] = tail{writer: end}
		}
		longest = max(longest, end)
	}
	return longest
}

// calibrateTicks measures TSC ticks per nanosecond over half a second, long
// enough that time.Now's 0.5ms Windows granularity is a 0.1% error.
func calibrateTicks() float64 {
	began, ticks := time.Now(), cputicks()
	time.Sleep(500 * time.Millisecond)
	return float64(cputicks()-ticks) / float64(time.Since(began).Nanoseconds())
}

func stampCost() int64 {
	const n = 1 << 20
	began := cputicks()
	for range n {
		cputicks()
	}
	return (cputicks() - began) / n
}

func formatNs(ns float64) string {
	switch {
	case ns >= 1e6:
		return fmt.Sprintf("%.2fms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.1fµs", ns/1e3)
	default:
		return fmt.Sprintf("%.0fns", ns)
	}
}
