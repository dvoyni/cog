package model

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
)

// A ClipMachine is a Component in waiting, so the ECS's own rule has
// to accept it: no funcs, no pointers, no bare slices.
func TestAClipMachineIsStorable(t *testing.T) {
	if err := ecs.Storable(reflect.TypeFor[ClipMachine]()); err != nil {
		t.Errorf("ecs.Storable(ClipMachine) = %v, want nil", err)
	}
}

// A copy shares the tables and nothing else: stepping and firing the copy
// leaves the original where it was.
func TestACopiedClipMachineStepsOnItsOwn(t *testing.T) {
	original, err := NewClipMachine(
		[]ClipInfo{{Name: "walk", Duration: 1}, {Name: "run", Duration: 1}},
		[]ClipState{
			{Name: "Walk", Clip: "walk", Loop: true},
			{Name: "Run", Clip: "run", Loop: true},
		},
		[]ClipTransition{{From: "Walk", To: "Run", On: "run", Crossfade: 1, Ease: EaseCubicOut}},
	)
	if err != nil {
		t.Fatalf("NewClipMachine: %v", err)
	}
	original.Step(0.25, nil)
	before := original.Plays(nil)

	copied := original
	if !copied.Fire("run") {
		t.Fatal("the copy found no run transition")
	}
	events := copied.Step(0.5, nil)
	if len(events) != 2 || events[1].Kind != ClipEntered || copied.StateName(events[1].State) != "Run" {
		t.Errorf("the copy's step said %+v, want Walk exited and Run entered", events)
	}
	if len(copied.Plays(nil)) != 2 {
		t.Errorf("the copy plays %+v, want a crossfade of two", copied.Plays(nil))
	}

	after := original.Plays(nil)
	if len(after) != 1 || after[0] != before[0] {
		t.Errorf("the original plays %+v after the copy moved, want %+v", after, before)
	}
	if events := original.Step(0, nil); len(events) != 0 {
		t.Errorf("the original picked up the copy's events %+v", events)
	}
}
