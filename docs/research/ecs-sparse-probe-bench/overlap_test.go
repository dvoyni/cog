package sbench

import (
	"context"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// This file validates the parallel_test.go harness rather than the design. If
// the "disjoint" regime did not genuinely run Systems concurrently, every
// shared-minus-disjoint number in that file would be measuring nothing at all.
//
// Each System burns a fixed span of wall time. Eight of them under locks nobody
// shares should finish in roughly one System's span; eight under one shared
// write lock should take roughly eight.

func sleepDisjoint[N any](d time.Duration) func() (kernel.Lock, kernel.Observe[sleepEvent]) {
	return func() (kernel.Lock, kernel.Observe[sleepEvent]) {
		var w kernel.Write[*tok[N]]
		return func(a kernel.ResourceAccess) {
				w = a.GetWrite[*tok[N]]()
			}, func(_ kernel.Kernel, _ sleepEvent) error {
				_ = w
				time.Sleep(d)
				return nil
			}
	}
}

func sleepShared[N any](d time.Duration) func() (kernel.Lock, kernel.Observe[sleepEvent]) {
	return func() (kernel.Lock, kernel.Observe[sleepEvent]) {
		var w kernel.Write[*sharedTok]
		return func(a kernel.ResourceAccess) {
				w = a.GetWrite[*sharedTok]()
			}, func(_ kernel.Kernel, _ sleepEvent) error {
				_ = w
				time.Sleep(d)
				return nil
			}
	}
}

type sleepEvent struct{}

type sleepSub[N any] kernel.Subscription[sleepEvent]

type sleepPlugin struct {
	shared bool
	d      time.Duration
}

func (sleepPlugin) Name() kernel.PluginName           { return "sleepbench" }
func (sleepPlugin) Dependencies() []kernel.PluginName { return nil }

func (p sleepPlugin) Register(r *kernel.Registrar, _ any) error {
	r.InitResource[*sharedTok](&sharedTok{})
	r.InitResource[*tok[ph0]](&tok[ph0]{})
	r.InitResource[*tok[ph1]](&tok[ph1]{})
	r.InitResource[*tok[ph2]](&tok[ph2]{})
	r.InitResource[*tok[ph3]](&tok[ph3]{})
	r.InitResource[*tok[ph4]](&tok[ph4]{})
	r.InitResource[*tok[ph5]](&tok[ph5]{})
	r.InitResource[*tok[ph6]](&tok[ph6]{})
	r.InitResource[*tok[ph7]](&tok[ph7]{})

	if p.shared {
		r.Subscribe[sleepSub[ph0], sleepEvent](sleepShared[ph0](p.d))
		r.Subscribe[sleepSub[ph1], sleepEvent](sleepShared[ph1](p.d))
		r.Subscribe[sleepSub[ph2], sleepEvent](sleepShared[ph2](p.d))
		r.Subscribe[sleepSub[ph3], sleepEvent](sleepShared[ph3](p.d))
		r.Subscribe[sleepSub[ph4], sleepEvent](sleepShared[ph4](p.d))
		r.Subscribe[sleepSub[ph5], sleepEvent](sleepShared[ph5](p.d))
		r.Subscribe[sleepSub[ph6], sleepEvent](sleepShared[ph6](p.d))
		r.Subscribe[sleepSub[ph7], sleepEvent](sleepShared[ph7](p.d))
		return nil
	}
	r.Subscribe[sleepSub[ph0], sleepEvent](sleepDisjoint[ph0](p.d))
	r.Subscribe[sleepSub[ph1], sleepEvent](sleepDisjoint[ph1](p.d))
	r.Subscribe[sleepSub[ph2], sleepEvent](sleepDisjoint[ph2](p.d))
	r.Subscribe[sleepSub[ph3], sleepEvent](sleepDisjoint[ph3](p.d))
	r.Subscribe[sleepSub[ph4], sleepEvent](sleepDisjoint[ph4](p.d))
	r.Subscribe[sleepSub[ph5], sleepEvent](sleepDisjoint[ph5](p.d))
	r.Subscribe[sleepSub[ph6], sleepEvent](sleepDisjoint[ph6](p.d))
	r.Subscribe[sleepSub[ph7], sleepEvent](sleepDisjoint[ph7](p.d))
	return nil
}

func TestDisjointSystemsActuallyOverlap(t *testing.T) {
	const per = 20 * time.Millisecond

	run := func(shared bool) time.Duration {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e := kernel.New(nil).
			Handler(func(err error) bool { t.Fatalf("kernel error: %v", err); return true }).
			WithPlugins(sleepPlugin{shared: shared, d: per})
		go e.Run(ctx)
		<-e.Ready()
		ex := e.Executioner()
		ex.PublishEvent(sleepEvent{}).Wait() // warm the publication plan
		start := time.Now()
		ex.PublishEvent(sleepEvent{}).Wait()
		return time.Since(start)
	}

	dis := run(false)
	sh := run(true)
	t.Logf("8 Systems x %v each: disjoint=%v shared=%v ratio=%.2fx",
		per, dis, sh, float64(sh)/float64(dis))

	if dis > 4*per {
		t.Fatalf("disjoint Systems did not overlap: %v for 8 x %v", dis, per)
	}
	if sh < 7*per {
		t.Fatalf("shared write lock did not serialise: %v for 8 x %v", sh, per)
	}
}
