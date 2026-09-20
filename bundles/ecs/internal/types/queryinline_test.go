package types

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// All()'s filler body is inlined into the closure All() returns rather than
// called from it, and that arrangement is load-bearing on a compiler budget
// nothing else in this tree watches. These two tests are that watch.
//
// The budget: a method gets 80 cost units, and every filler is far past it —
// iterate1 is 186 and iterate2 is 331, with queryCursor.fill alone at 67 and
// appearing two to four times in each. A func literal created and called
// exactly once gets 800 instead (inlineClosureCalledOnceCost), which is the
// only reason the body fits anywhere at all.
//
// Busting 800 is not a lost optimisation, it is a 2.3x regression: the closure
// then compiles as a standalone body that also loses the row and fill inlining
// today's iterate2 keeps, measured at +4.8 ns an Entity. The allocation-line
// tests do not see it — under a busted build they pass unchanged at 200 B/op
// and 4 allocs/op, because the regression allocates nothing. Nothing else in
// the suite would catch it either, so it would ship.

// closureBudget is the compiler's budget for a func literal created and called
// exactly once. It is a compiler-internal constant rather than a language
// guarantee, which is the other reason these tests exist: if a Go release
// changes it, they fail here rather than silently in a frame.
const closureBudget = 800

var (
	inlineOnce   sync.Once
	inlineOutput string
)

// verdict splits a -m=2 line into the position it is about and what the
// compiler said. A position carries no spaces; everything after it does, since
// an instantiated name spells its type arguments out in full.
var verdict = regexp.MustCompile(`^(\S+\.go:\d+:\d+): (.*)$`)

// The three sentences this test reads. Everything else -m=2 prints — escape
// analysis, leak paths, flow — is noise here.
const (
	accepted = "can inline "
	refused  = "cannot inline "
	inlined  = "inlining call to "
)

// inlineVerdicts builds this package's test binary under -m=2 and returns what
// the compiler said. It shells out the way imports_test.go does, because the
// verdicts exist only at compile time and there is no other way to read them.
//
// The build is cached, and a cached build replays its diagnostics, so the
// second caller pays nothing.
func inlineVerdicts(t *testing.T) string {
	t.Helper()
	inlineOnce.Do(func() {
		command := exec.Command("go", "test", "-c",
			"-o", filepath.Join(t.TempDir(), "inline.test"),
			"-gcflags=github.com/dvoyni/cog/bundles/ecs/internal/types=-m=2",
			".",
		)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("go test -c: %v\n%s", err, out)
		}
		inlineOutput = string(out)
	})
	if inlineOutput == "" {
		t.Fatal("no compiler output: the build reported nothing under -m=2")
	}
	return inlineOutput
}

// allFunc1 reports whether a function name is the literal All() returns for a
// Query, as against the one List or Hooks returns.
func allFunc1(name string) bool {
	return strings.Contains(name, "(*Query[") && strings.HasSuffix(name, ".All.func1")
}

// inlineSentence splits one -m=2 line into its position, which of the three
// sentences it is, and the function it names. A "can inline" line carries the
// whole inlined body after "as:", and a "cannot inline" line carries the cost
// and the budget; both are trimmed off the name here and read by the caller
// from the tail it is handed.
func inlineSentence(line string) (position, sentence, name, tail string, ok bool) {
	match := verdict.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return "", "", "", "", false
	}
	position, rest := match[1], match[2]
	switch {
	case strings.HasPrefix(rest, accepted):
		// can inline <name> with cost <n> as: <body>
		rest = strings.TrimPrefix(rest, accepted)
		if body := strings.Index(rest, " as: "); body >= 0 {
			rest = rest[:body]
		}
		cut := strings.LastIndex(rest, " with cost ")
		if cut < 0 {
			return "", "", "", "", false
		}
		return position, accepted, rest[:cut], rest[cut+len(" with cost "):], true
	case strings.HasPrefix(rest, refused):
		// cannot inline <name>: <reason>, where the reason is "cost <n> exceeds
		// budget <m>" or that behind "function too complex: ". The name itself
		// never carries a ": ", however fat its type arguments are, so the
		// first one is the separator — and the reason has to be split off by
		// the first, not by ": cost ", or "function too complex" is read as
		// part of the name and the verdict is missed entirely.
		rest = strings.TrimPrefix(rest, refused)
		cut := strings.Index(rest, ": ")
		if cut < 0 {
			return "", "", "", "", false
		}
		return position, refused, rest[:cut], rest[cut+len(": "):], true
	case strings.HasPrefix(rest, inlined):
		return position, inlined, strings.TrimPrefix(rest, inlined), "", true
	}
	return "", "", "", "", false
}

// costAndBudget reads the numbers off a refusal's tail, which reads either
// "cost 817 exceeds budget 800" or the same behind "function too complex: ".
func costAndBudget(tail string) (cost, budget string) {
	fields := strings.Fields(strings.TrimPrefix(tail, "function too complex: "))
	if len(fields) < 5 {
		return tail, ""
	}
	return fields[1], fields[4]
}

// TestAllsFillerStaysInsideTheClosureBudget is the net under the arrangement
// itself: every instantiation of the literal All() returns must still inline.
// A refusal here is the 2.3x regression, and the cost it reports is how far
// over the edit went.
func TestAllsFillerStaysInsideTheClosureBudget(t *testing.T) {
	seen, refusals, worst, worstName := 0, 0, 0, ""
	for line := range strings.SplitSeq(inlineVerdicts(t), "\n") {
		position, sentence, name, tail, ok := inlineSentence(line)
		if !ok || !allFunc1(name) {
			continue
		}
		switch sentence {
		case refused:
			refusals++
			cost, budget := costAndBudget(tail)
			t.Errorf("%s: All()'s literal no longer inlines: cost %s exceeds budget %s\n"+
				"The filler body in All() has outgrown the called-once closure budget. Every range\n"+
				"site over this Query shape now pays an indirect call an Entity and loses the row\n"+
				"and fill inlining besides, measured at +4.8 ns an Entity. Move a body back out.\n"+
				"The shape is %s",
				position, cost, budget, name)
		case accepted:
			cost, err := strconv.Atoi(strings.TrimSpace(tail))
			if err != nil {
				t.Fatalf("%s: unreadable cost %q", position, tail)
			}
			seen++
			if cost > worst {
				worst, worstName = cost, name
			}
			if cost > closureBudget {
				t.Errorf("%s: All()'s literal costs %d against a budget of %d", position, cost, closureBudget)
			}
		}
	}
	// Nothing accepted and nothing refused means the output was not what this
	// test thinks it is, which is a different failure from a blown budget and
	// has to read as one. Refusals alone have already been reported above.
	if seen == 0 && refusals == 0 {
		t.Fatal("no Query All.func1 instantiation appeared in the compiler's output, so this test proved nothing")
	}
	if seen == 0 {
		return
	}
	t.Logf("%d instantiations, worst %d of %d (%d units of headroom): %s",
		seen, worst, closureBudget, closureBudget-worst, worstName)
}

// TestAShapeTwoRangeBodyInlinesIntoTheWalk is the net under the win. Inlining
// the literal is necessary but not sufficient: it already inlined before this
// change, at cost 64, while delegating to a filler that did not. What the
// change buys is the step after — with the filler's body inside the literal,
// the compiler goes on to inline the range statement's own yield closure into
// the walk, so the loop runs with no per-Entity call at all.
//
// That second step is the whole 0.89 ns an Entity, and it is visible only as an
// "inlining call to <caller>-range1" at the same position as the literal's own.
func TestAShapeTwoRangeBodyInlinesIntoTheWalk(t *testing.T) {
	queried := map[string]bool{}
	ranged := map[string][]string{}
	for line := range strings.SplitSeq(inlineVerdicts(t), "\n") {
		position, sentence, name, _, ok := inlineSentence(line)
		if !ok || sentence != inlined {
			continue
		}
		switch {
		case allFunc1(name):
			queried[position] = true
		case strings.Contains(name, "-range"):
			ranged[position] = append(ranged[position], name)
		}
	}
	if len(queried) == 0 {
		t.Fatal("no range site inlined a Query's All.func1.\n" +
			"If TestAllsFillerStaysInsideTheClosureBudget also failed, that one names the cause:\n" +
			"a literal over budget inlines nowhere, so no site can collapse its body either.")
	}
	collapsed := make([]string, 0, len(queried))
	for site := range queried {
		if len(ranged[site]) > 0 {
			collapsed = append(collapsed, site)
		}
	}
	if len(collapsed) == 0 {
		t.Errorf("no range site collapsed its body into the walk, across %d site(s) that inlined a Query's All.func1.\n"+
			"Every shape-2 site should report an \"inlining call to <caller>-range1\" beside the literal's own.\n"+
			"Without it the yield is an indirect call an Entity and the walk is back to what it cost before.",
			len(queried))
		return
	}
	t.Logf("%d of %d Query range sites collapsed the body into the walk: %s",
		len(collapsed), len(queried), strings.Join(collapsed, ", "))
}
