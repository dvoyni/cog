package wgpu

import (
	"context"
	"testing"
)

func TestQuitOnCancellationStopsWhenRunCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	watcherDone := make(chan struct{})
	quit := make(chan struct{}, 1)
	go func() {
		quitOnCancellation(ctx, runDone, func() { quit <- struct{}{} })
		close(watcherDone)
	}()

	close(runDone)
	<-watcherDone
	cancel()
	select {
	case <-quit:
		t.Fatal("Quit called after the host run completed")
	default:
	}
}

func TestQuitOnCancellationQuitsActiveRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	quit := make(chan struct{}, 1)
	watcherDone := make(chan struct{})
	go func() {
		quitOnCancellation(ctx, runDone, func() { quit <- struct{}{} })
		close(watcherDone)
	}()

	cancel()
	<-watcherDone
	select {
	case <-quit:
	default:
		t.Fatal("Quit was not called for an active host run")
	}
}
