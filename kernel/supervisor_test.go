package kernel

import (
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestSpawnAssignsFreshIDs(t *testing.T) {
	s := NewSupervisor(nil)
	first, err := s.Spawn(nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Spawn(nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first == 0 || second <= first {
		t.Fatalf("Spawn answered %d then %d, want increasing non-zero IDs", first, second)
	}
	if k := s.GetKernel(first); k == nil || k.ID() != first {
		t.Fatalf("GetKernel(%d) answered %v", first, k)
	}
}

func TestDestroyRemovesKernel(t *testing.T) {
	s := NewSupervisor(nil)
	id, _ := s.Spawn(nil, 2)
	s.Destroy(id)
	if s.GetKernel(id) != nil {
		t.Fatal("GetKernel answered a kernel being destroyed")
	}
	s.Destroy(id) // not live: does nothing
	select {
	case <-s.Done():
		t.Fatal("destroying the last kernel closed Done")
	default:
	}
}

func TestQuitClosesDoneOnceKernelsAreGone(t *testing.T) {
	s := NewSupervisor(nil)
	for range 3 {
		if _, err := s.Spawn(nil, 2); err != nil {
			t.Fatal(err)
		}
	}
	s.Quit()
	s.Quit()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed")
	}
	if len(s.kernels) != 0 {
		t.Fatal("kernels left after Done")
	}
	if _, err := s.Spawn(nil, 2); !errors.Is(err, ErrQuitting) {
		t.Fatalf("Spawn after Quit answered %v, want ErrQuitting", err)
	}
	if s.WaitMainThread() {
		t.Fatal("WaitMainThread answered true after Done")
	}
}

func TestQuitWithNoKernelsClosesDone(t *testing.T) {
	s := NewSupervisor(nil)
	s.Quit()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed")
	}
}

func TestBroadcastFollowsSpawnAndDestroy(t *testing.T) {
	s := NewSupervisor(nil)
	first, _ := s.Spawn(nil, 2)
	second, _ := s.Spawn(nil, 2)
	third, _ := s.Spawn(nil, 2)
	before := s.spawnOrder
	s.Destroy(second)
	ids := []KernelID{}
	for _, k := range s.spawnOrder {
		ids = append(ids, k.ID())
	}
	if !slices.Equal(ids, []KernelID{first, third}) {
		t.Fatalf("spawn order after destroy is %v, want [%d %d]", ids, first, third)
	}
	if len(before) != 3 || before[1].ID() != second {
		t.Fatal("destroy changed a spawn order a Broadcast may still hold")
	}
	if got := testing.AllocsPerRun(100, func() { s.Broadcast(struct{ Value int32 }{1}) }); got != 0 {
		t.Fatalf("Broadcast allocated %v times", got)
	}
}

func TestPinnedJobWakesMainThread(t *testing.T) {
	s := NewSupervisor(nil)
	id, _ := s.Spawn(nil, 2)
	woke := make(chan bool)
	go func() { woke <- s.WaitMainThread() }()
	s.pushPinned(s.GetKernel(id), JobHandle{})
	select {
	case ok := <-woke:
		if !ok {
			t.Fatal("WaitMainThread answered false before Done")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitMainThread never woke")
	}
}

func TestWaitMainThreadWakesOnQuit(t *testing.T) {
	s := NewSupervisor(nil)
	woke := make(chan bool)
	go func() { woke <- s.WaitMainThread() }()
	s.Quit()
	select {
	case ok := <-woke:
		if ok {
			t.Fatal("WaitMainThread answered true on Quit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitMainThread never woke")
	}
}

func TestConcurrentSpawnDestroyQuit(t *testing.T) {
	s := NewSupervisor(nil)
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			for range 50 {
				id, err := s.Spawn(nil, 2)
				if err != nil {
					return // quitting
				}
				s.Broadcast(struct{ Value int32 }{1})
				s.Destroy(id)
			}
		})
	}
	time.Sleep(time.Millisecond)
	s.Quit()
	wait.Wait()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed")
	}
}

func TestSpawnWithNoWorkersPanics(t *testing.T) {
	expectPanic(t, "no workers", func() { NewSupervisor(nil).Spawn(nil, 0) })
}

func TestSpawnHandsEachPluginItsPartOfConfig(t *testing.T) {
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	received := map[PluginName]any{}
	receiver := func(name PluginName) hookPlugin {
		return hookPlugin{name: name, receive: func(config any) { received[name] = config }}
	}
	config := map[PluginName]any{"physics": 1, "render": "vulkan"}
	if _, err := s.Spawn(config, 2, receiver("physics"), receiver("render"), receiver("audio")); err != nil {
		t.Fatal(err)
	}
	want := map[PluginName]any{"physics": 1, "render": "vulkan", "audio": nil}
	if !maps.Equal(received, want) {
		t.Fatalf("plugins received %v, want %v", received, want)
	}
}

func TestSpawnWithTwoPluginsOfOneNamePanics(t *testing.T) {
	expectPanic(t, "two plugins of one name", func() {
		NewSupervisor(nil).Spawn(nil, 2, hookPlugin{name: "physics"}, hookPlugin{name: "physics"})
	})
}

func TestSpawnPanicsOnADependencyNotListedBeforeNamingBothPlugins(t *testing.T) {
	cases := []struct {
		what    string
		plugins []Plugin
		want    string
	}{
		{"a dependency listed later", []Plugin{
			hookPlugin{name: "render", dependencies: []PluginName{"platform"}},
			hookPlugin{name: "platform"},
		}, `"render"` + "\x00" + `"platform"` + "\x00" + "after"},
		{"a missing dependency", []Plugin{
			hookPlugin{name: "render", dependencies: []PluginName{"platform"}},
		}, `"render"` + "\x00" + `"platform"` + "\x00" + "not given"},
		{"a plugin naming itself", []Plugin{
			hookPlugin{name: "render", dependencies: []PluginName{"render"}},
		}, `"render"` + "\x00" + "itself"},
		{"a dependency named twice", []Plugin{
			hookPlugin{name: "platform"},
			hookPlugin{name: "render", dependencies: []PluginName{"platform", "platform"}},
		}, `"render"` + "\x00" + `"platform"` + "\x00" + "twice"},
	}
	for _, test := range cases {
		t.Run(test.what, func(t *testing.T) {
			expectPanicSaying(t, test.want, func() { NewSupervisor(nil).Spawn(nil, 2, test.plugins...) })
		})
	}
}

func TestSpawnPanicsOnABadDependencyBeforeAnyInit(t *testing.T) {
	var calls []string
	expectPanic(t, "a dependency listed later", func() {
		NewSupervisor(nil).Spawn(nil, 2,
			calledPlugin{name: "platform", calls: &calls},
			calledPlugin{name: "render", dependencies: []PluginName{"audio"}, calls: &calls},
			calledPlugin{name: "audio", calls: &calls},
		)
	})
	if len(calls) != 0 {
		t.Fatalf("calls are %v, want none before the panic", calls)
	}
}

func TestDependenciesLeaveThePluginOrderAsGiven(t *testing.T) {
	var calls []string
	s := NewSupervisor(nil)
	_, err := s.Spawn(nil, 2,
		calledPlugin{name: "audio", calls: &calls},
		calledPlugin{name: "platform", calls: &calls},
		calledPlugin{name: "render", dependencies: []PluginName{"platform"}, calls: &calls},
		calledPlugin{name: "game", dependencies: []PluginName{"render", "audio"}, calls: &calls},
	)
	if err != nil {
		t.Fatal(err)
	}
	s.Quit()
	<-s.Done()
	want := []string{
		"audio.Init", "platform.Init", "render.Init", "game.Init",
		"audio.Start", "platform.Start", "render.Start", "game.Start",
		"game.Stop", "render.Stop", "platform.Stop", "audio.Stop",
		"game.Destroy", "render.Destroy", "platform.Destroy", "audio.Destroy",
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("calls are %v, want %v", calls, want)
	}
}
