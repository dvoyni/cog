//go:build kernel_load

package kernel

import (
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The lock load test, ported from v1's TestSchedulerLockOverhead: a frame of
// loadSystems systems over loadComponents resources, each holding its locks
// for a fixed spin, published as one batch and run by the kernel's workers.
//
//	go test -tags kernel_load -run TestLockLoad -v ./kernel
//
// v2 has no scheduler trace, so it reports what v1's did at the top: the
// frame's wall time against its critical path, the time it would take if
// scheduling cost nothing and every system had a core. The workload is v1's,
// from the same seed.
const (
	loadComponents = 200
	loadSystems    = 300
	loadFrames     = 200
	loadWarmup     = 20
	loadMinWork    = 30 * time.Nanosecond
	loadMaxWork    = time.Millisecond
)

// loadEpoch is what the load test's stamps count from.
var loadEpoch = time.Now()

func loadNow() int64 { return int64(time.Since(loadEpoch)) }

// loadRun is one system's execution in one frame, in nanoseconds.
type loadRun struct {
	woke, ended int64
}

// loadSystem is one system: its locks, its work, and the run it is recording.
type loadSystem struct {
	reads, writes []int
	work          int64
	current       *loadRun
}

type loadFrameEvent struct{}

// loadMode is how a load test spawns its kernels.
type loadMode struct {
	name        string
	workerCount int
}

// loadModes are the ways a load test runs: a worker per core, and one
// worker running every job in turn.
var loadModes = []loadMode{
	{"workers", runtime.GOMAXPROCS(0)},
	{"one worker", 1},
}

// loadKernels is how many kernels TestLockLoadKernels runs at once.
const loadKernels = 16

// loadKernel is one kernel running the load workload.
type loadKernel struct {
	kernel  *Kernel
	systems []*loadSystem
	sig     *Signal
}

// spawnLoadKernel spawns a kernel on s whose one plugin registers the load
// workload: every component, a task per system, and a handler scheduling
// every system as one frame.
func spawnLoadKernel(t *testing.T, s *Supervisor, mode loadMode) *loadKernel {
	t.Helper()
	load := &loadKernel{systems: loadWorkload(), sig: NewSignal()}
	id, err := s.Spawn(nil, mode.workerCount, hookPlugin{init: func(c *Configurator) {
		for _, initComponent := range initComponents {
			initComponent(c)
		}
		tasks := make([]*Task[NoArg, NoResult], len(load.systems))
		for i, system := range load.systems {
			tasks[i] = c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
				for _, index := range system.reads {
					lockComponents[index](access, false)
				}
				for _, index := range system.writes {
					lockComponents[index](access, true)
				}
				return func(k *Kernel, arg NoArg) NoResult {
					run := system.current
					run.woke = loadNow()
					for end := run.woke + system.work; loadNow() < end; {
					}
					run.ended = loadNow()
					return NoResult{}
				}
			})
		}
		handles := make([]JobHandle, len(tasks))
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e loadFrameEvent) (loadFrameEvent, JobHandle) {
			for i, task := range tasks {
				handles[i] = ex.Schedule(task, NoArg{}).Handle()
			}
			return e, ex.Join(handles...)
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	load.kernel = s.GetKernel(id)
	return load
}

// frame publishes one frame, recording each system's run into runs, waits for
// it, and answers its wall time.
func (l *loadKernel) frame(runs []loadRun) int64 {
	for i, system := range l.systems {
		system.current = &runs[i]
	}
	began := loadNow()
	_, out := l.kernel.Publish(loadFrameEvent{})
	l.kernel.Notify(l.sig, out)
	<-l.sig.C()
	return loadNow() - began
}

func TestLockLoad(t *testing.T) {
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	load := spawnLoadKernel(t, s, loadModes[0])
	for range loadWarmup {
		load.frame(make([]loadRun, len(load.systems)))
	}
	walls := make([]int64, loadFrames)
	bounds := make([]int64, loadFrames)
	for f := range loadFrames {
		runs := make([]loadRun, len(load.systems))
		walls[f] = load.frame(runs)
		bounds[f] = criticalPath(load.systems, runs)
	}
	t.Log("\n" + loadReport(walls, bounds))
}

// TestLockLoadKernels runs TestLockLoad's frames on loadKernels kernels at
// once, every kernel on its own goroutine, in every loadMode. With more kernels than cores the CPU bounds the
// run, so it reports the elapsed time against the larger of the total work
// spread over every core and the longest kernel's chain of critical paths.
//
//	go test -tags kernel_load -run TestLockLoadKernels -v ./kernel
func TestLockLoadKernels(t *testing.T) {
	for _, mode := range loadModes {
		t.Run(mode.name, func(t *testing.T) {
			s := NewSupervisor(nil)
			t.Cleanup(s.Quit)
			loads := make([]*loadKernel, loadKernels)
			for i := range loads {
				loads[i] = spawnLoadKernel(t, s, mode)
			}
			walls := make([][]int64, loadKernels)
			bounds := make([]int64, loadKernels)
			works := make([]int64, loadKernels)
			var warmed, finished sync.WaitGroup
			warmed.Add(loadKernels)
			start := make(chan struct{})
			for i, load := range loads {
				finished.Go(func() {
					for range loadWarmup {
						load.frame(make([]loadRun, len(load.systems)))
					}
					warmed.Done()
					<-start
					for range loadFrames {
						runs := make([]loadRun, len(load.systems))
						walls[i] = append(walls[i], load.frame(runs))
						bounds[i] += criticalPath(load.systems, runs)
						// The work asked for, not the time measured: a job preempted
						// mid-spin counts time it spent off a core.
						for _, system := range load.systems {
							works[i] += system.work
						}
					}
				})
			}
			warmed.Wait()
			began := loadNow()
			close(start)
			finished.Wait()
			elapsed := loadNow() - began
			t.Log("\n" + kernelsReport(mode.name, walls, bounds, works, elapsed))
		})
	}
}

// kernelsReport reports a TestLockLoadKernels run: the elapsed time against
// its ideal, and every kernel's frames together.
func kernelsReport(mode string, walls [][]int64, bounds, works []int64, elapsed int64) string {
	var allWalls []int64
	var work, longestChain int64
	for i := range walls {
		allWalls = append(allWalls, walls[i]...)
		work += works[i]
		longestChain = max(longestChain, bounds[i])
	}
	cores := int64(runtime.GOMAXPROCS(0))
	ideal := max(work/cores, longestChain)
	sorted := slices.Sorted(slices.Values(allWalls))
	var wallSum int64
	for _, wall := range sorted {
		wallSum += wall
	}
	at := func(q float64) int64 { return sorted[min(len(sorted)-1, int(q*float64(len(sorted))))] }
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d kernels x %d systems x %d frames, %d cores\n", mode, len(walls), loadSystems, loadFrames, cores)
	fmt.Fprintf(&out, "elapsed: %s; ideal %s (work %s over %d cores, longest kernel chain %s); efficiency %.1f%%\n",
		formatNs(float64(elapsed)), formatNs(float64(ideal)), formatNs(float64(work)), cores, formatNs(float64(longestChain)),
		100*float64(ideal)/float64(elapsed))
	fmt.Fprintf(&out, "frame wall per kernel: mean %s, p50 %s, p90 %s, p99 %s, max %s\n",
		formatNs(float64(wallSum)/float64(len(sorted))), formatNs(float64(at(0.5))), formatNs(float64(at(0.9))),
		formatNs(float64(at(0.99))), formatNs(float64(sorted[len(sorted)-1])))
	return out.String()
}

// loadWorkload builds the systems as v1's did: the same seed, so the same lock
// sets and the same work.
func loadWorkload() []*loadSystem {
	rng := rand.New(rand.NewPCG(1, 2))
	systems := make([]*loadSystem, loadSystems)
	for i := range systems {
		picked := rng.Perm(loadComponents)
		writes, reads := 1+rng.IntN(2), 1+rng.IntN(5)
		system := &loadSystem{
			writes: slices.Sorted(slices.Values(picked[:writes])),
			reads:  slices.Sorted(slices.Values(picked[writes : writes+reads])),
		}
		// Log-uniform: most systems are short, a few are long, as in a game.
		logMin, logMax := math.Log(float64(loadMinWork)), math.Log(float64(loadMaxWork))
		system.work = int64(math.Exp(logMin + rng.Float64()*(logMax-logMin)))
		systems[i] = system
	}
	return systems
}

// criticalPath is how long one frame would take if scheduling cost nothing
// and every system had a core: each system starts the moment the systems it
// conflicts with before it have finished, in the order they were scheduled,
// with the work each actually did. It is v1's rule.
func criticalPath(systems []*loadSystem, runs []loadRun) int64 {
	// tail is a resource's state in finish times: when its writer finishes, and
	// when the last reader since that writer finishes.
	type tail struct{ writer, readers int64 }
	tracks := make([]tail, loadComponents)
	var longest int64
	for i, system := range systems {
		var start int64
		for _, index := range system.reads {
			start = max(start, tracks[index].writer)
		}
		for _, index := range system.writes {
			start = max(start, tracks[index].writer, tracks[index].readers)
		}
		end := start + (runs[i].ended - runs[i].woke)
		for _, index := range system.reads {
			tracks[index].readers = max(tracks[index].readers, end)
		}
		for _, index := range system.writes {
			tracks[index] = tail{writer: end}
		}
		longest = max(longest, end)
	}
	return longest
}

func loadReport(walls, bounds []int64) string {
	overheads := make([]int64, len(walls))
	var wallSum, boundSum int64
	for i := range walls {
		overheads[i] = walls[i] - bounds[i]
		wallSum += walls[i]
		boundSum += bounds[i]
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%d components, %d systems, %d frames (after %d warmup), %d workers\n",
		loadComponents, loadSystems, loadFrames, loadWarmup, runtime.GOMAXPROCS(0))
	fmt.Fprintf(&out, "frame wall time: %s mean; %s with no overhead and unlimited cores\n\n",
		formatNs(float64(wallSum)/loadFrames), formatNs(float64(boundSum)/loadFrames))
	fmt.Fprintf(&out, "%-30s %10s %10s %10s %10s %10s\n", "per frame", "mean", "p50", "p90", "p99", "max")
	row := func(name string, values []int64) {
		sorted := slices.Sorted(slices.Values(values))
		var sum int64
		for _, v := range sorted {
			sum += v
		}
		at := func(q float64) int64 { return sorted[min(len(sorted)-1, int(q*float64(len(sorted))))] }
		fmt.Fprintf(&out, "%-30s %10s %10s %10s %10s %10s\n", name,
			formatNs(float64(sum)/float64(len(sorted))), formatNs(float64(at(0.5))), formatNs(float64(at(0.9))),
			formatNs(float64(at(0.99))), formatNs(float64(sorted[len(sorted)-1])))
	}
	row("wall", walls)
	row("critical path", bounds)
	row("overhead (wall - critical)", overheads)
	return out.String()
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

// The light load test: what a server runs, many kernels each ticked at a
// fixed rate with a little work. It reports how long a tick takes from
// publish to its last job, and how much CPU the whole process spent, since
// on a mostly idle server CPU is the cost.
const (
	lightKernels  = 64
	lightTicks    = 500
	lightInterval = time.Millisecond
	lightJobs     = 8
	lightWork     = 2 * time.Microsecond
)

type lightTickEvent struct{}

// spawnLightKernel spawns a kernel on s whose every tick schedules lightJobs
// jobs, each spinning lightWork while writing one of four resources, so some
// of them wait on one another.
func spawnLightKernel(t *testing.T, s *Supervisor, mode loadMode) *Kernel {
	t.Helper()
	id, err := s.Spawn(nil, mode.workerCount, hookPlugin{init: func(c *Configurator) {
		for _, initComponent := range initComponents[:4] {
			initComponent(c)
		}
		tasks := make([]*Task[NoArg, NoResult], lightJobs)
		for i := range tasks {
			tasks[i] = c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
				lockComponents[i%4](access, true)
				return func(k *Kernel, arg NoArg) NoResult {
					for end := loadNow() + int64(lightWork); loadNow() < end; {
					}
					return NoResult{}
				}
			})
		}
		var handles [lightJobs]JobHandle
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e lightTickEvent) (lightTickEvent, JobHandle) {
			for i, task := range tasks {
				handles[i] = ex.Schedule(task, NoArg{}).Handle()
			}
			return e, ex.Join(handles[:]...)
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s.GetKernel(id)
}

// go test -tags kernel_load -run TestLightLoadKernels -v ./kernel
func TestLightLoadKernels(t *testing.T) {
	for _, mode := range loadModes {
		t.Run(mode.name, func(t *testing.T) {
			s := NewSupervisor(nil)
			t.Cleanup(s.Quit)
			kernels := make([]*Kernel, lightKernels)
			for i := range kernels {
				kernels[i] = spawnLightKernel(t, s, mode)
			}
			latencies := make([][]int64, lightKernels)
			var finished sync.WaitGroup
			cpuBefore := processCPU()
			began := loadNow()
			for i, k := range kernels {
				finished.Go(func() {
					sig := NewSignal()
					ticker := time.NewTicker(lightInterval)
					defer ticker.Stop()
					for range lightTicks {
						<-ticker.C
						published := loadNow()
						_, out := k.Publish(lightTickEvent{})
						k.Notify(sig, out)
						<-sig.C()
						latencies[i] = append(latencies[i], loadNow()-published)
					}
				})
			}
			finished.Wait()
			elapsed := loadNow() - began
			cpu := processCPU() - cpuBefore

			var all []int64
			for _, kernel := range latencies {
				all = append(all, kernel...)
			}
			sorted := slices.Sorted(slices.Values(all))
			var sum int64
			for _, latency := range sorted {
				sum += latency
			}
			at := func(q float64) int64 { return sorted[min(len(sorted)-1, int(q*float64(len(sorted))))] }
			ticks := int64(len(sorted))
			work := ticks * lightJobs * int64(lightWork)
			t.Logf("\n%s: %d kernels x %d ticks every %s, %d jobs of %s each\n"+
				"tick latency: mean %s, p50 %s, p90 %s, p99 %s, max %s\n"+
				"CPU: %s over %s elapsed (%.2f cores); %s per tick, of which %s is work\n",
				mode.name, lightKernels, lightTicks, lightInterval, lightJobs, lightWork,
				formatNs(float64(sum)/float64(ticks)), formatNs(float64(at(0.5))), formatNs(float64(at(0.9))),
				formatNs(float64(at(0.99))), formatNs(float64(sorted[len(sorted)-1])),
				formatNs(float64(cpu)), formatNs(float64(elapsed)), float64(cpu)/float64(elapsed),
				formatNs(float64(cpu)/float64(ticks)), formatNs(float64(work)/float64(ticks)))
		})
	}
}

// processCPU answers the CPU time the process has used, user and system, in
// nanoseconds.
func processCPU() int64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		panic(err)
	}
	return usage.Utime.Nano() + usage.Stime.Nano()
}
