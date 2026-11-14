package rendertest_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

func TestAReportOfAKindTheTestExpectsFailsNothing(t *testing.T) {
	fake := &fakeTest{TB: t}
	h := rendertest.NewHarness(fake)
	h.ExpectReport(1)
	h.Spawn(nil, &failingPlugin{errorsToLog: []error{render.Report{Kind: 1, Err: errors.New("render: an expected mistake")}}})

	h.Step()

	if failures := fake.takeFailures(); len(failures) != 0 {
		t.Errorf("the harness failed the test with %q, want no failure", failures)
	}
}

func TestAReportOfAKindTheTestDidNotNameFailsIt(t *testing.T) {
	fake := &fakeTest{TB: t}
	h := rendertest.NewHarness(fake)
	h.ExpectReport(1)
	h.Spawn(nil, &failingPlugin{errorsToLog: []error{
		render.Report{Kind: 1, Err: errors.New("render: an expected mistake")},
		fmt.Errorf("a wrapped %w", render.Report{Kind: 2, Err: errors.New("render: an unexpected mistake")}),
	}})

	h.Step()

	if failures := fake.takeFailures(); len(failures) != 1 {
		t.Errorf("the harness failed the test with %q, want one failure, for the report of kind 2", failures)
	}
}

func TestAnErrorThatIsNoReportFailsTheTest(t *testing.T) {
	fake := &fakeTest{TB: t}
	h := rendertest.NewHarness(fake)
	h.Spawn(nil, &failingPlugin{errorsToLog: []error{errors.New("a plugin's failure")}})

	h.Step()

	if failures := fake.takeFailures(); len(failures) != 1 {
		t.Errorf("the harness failed the test with %q, want one failure", failures)
	}
}
