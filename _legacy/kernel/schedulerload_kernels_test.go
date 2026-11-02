//go:build kernel_schedtrace

package kernel

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// loadKernels is how many schedulers TestSchedulerLockOverheadKernels runs at
// once, one per kernel, as v2's TestLockLoadKernels runs kernels.
const loadKernels = 16

// TestSchedulerLockOverheadKernels runs TestSchedulerLockOverhead's pool frames
// on loadKernels schedulers at once, each on its own goroutine. With more
// kernels than cores the CPU bounds the run, so it reports the elapsed time
// against the larger of the total work spread over every core and the longest
// scheduler's chain of critical paths.
//
//	go test -tags kernel_schedtrace -run TestSchedulerLockOverheadKernels -v ./kernel
func TestSchedulerLockOverheadKernels(t *testing.T) {
	ticksPerNs := calibrateTicks()
	schedulers := make([]*scheduler, loadKernels)
	systems := make([][]*loadTask, loadKernels)
	for i := range schedulers {
		schedulers[i] = newScheduler()
		defer schedulers[i].shutdown()
		systems[i] = loadWorkload(ticksPerNs)
	}
	walls := make([][]int64, loadKernels)
	bounds := make([]int64, loadKernels)
	works := make([]int64, loadKernels)
	var warmed, finished sync.WaitGroup
	warmed.Add(loadKernels)
	start := make(chan struct{})
	for i := range schedulers {
		finished.Go(func() {
			for range loadWarmup {
				poolFrame(schedulers[i], systems[i], make([]loadRun, len(systems[i])))
			}
			warmed.Done()
			<-start
			for range loadFrames {
				runs := make([]loadRun, len(systems[i]))
				began := cputicks()
				poolFrame(schedulers[i], systems[i], runs)
				walls[i] = append(walls[i], cputicks()-began)
				bounds[i] += criticalPath(systems[i], runs)
				// The work asked for, not the time measured: a job preempted
				// mid-spin counts time it spent off a core.
				for _, system := range systems[i] {
					works[i] += system.work
				}
			}
		})
	}
	warmed.Wait()
	began := cputicks()
	close(start)
	finished.Wait()
	elapsed := cputicks() - began

	ns := func(ticks int64) string { return formatNs(float64(ticks) / ticksPerNs) }
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
	fmt.Fprintf(&out, "pool: %d kernels x %d systems x %d frames, %d cores\n", loadKernels, loadSystems, loadFrames, cores)
	fmt.Fprintf(&out, "elapsed: %s; ideal %s (work %s over %d cores, longest kernel chain %s); efficiency %.1f%%\n",
		ns(elapsed), ns(ideal), ns(work), cores, ns(longestChain), 100*float64(ideal)/float64(elapsed))
	fmt.Fprintf(&out, "frame wall per kernel: mean %s, p50 %s, p90 %s, p99 %s, max %s\n",
		ns(wallSum/int64(len(sorted))), ns(at(0.5)), ns(at(0.9)), ns(at(0.99)), ns(sorted[len(sorted)-1]))
	t.Log("\n" + out.String())
}
