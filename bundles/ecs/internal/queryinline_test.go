package internal

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The walks are written out inside the closure All() returns rather than
// called from it, and that arrangement is load-bearing on a compiler budget
// nothing else in this tree watches. These tests are that watch.
//
// The budget: a method gets 80 cost units, and every filler is far past it —
// iterate1 is 196 and iterate2 is 343, with queryCursor.fill alone at 67 and
// appearing one to four times in each. A func literal created and called
// exactly once gets 800 instead (inlineClosureCalledOnceCost), which is the
// only reason a walk fits anywhere at all.
//
// Busting 800 is not a lost optimisation, it is a 2.3x regression: the closure
// then compiles as a standalone body that also loses the row and fill inlining
// the fillers keep, measured at +4.8 ns an Entity. The allocation-line tests do
// not see it — under a busted build they pass unchanged at 200 B/op and 4
// allocs/op, because the regression allocates nothing. Nothing else in the
// suite would catch it either, so it would ship.

// closureBudget is the compiler's budget for a func literal created and called
// exactly once. It is a compiler-internal constant rather than a language
// guarantee, which is the other reason these tests exist: if a Go release
// changes it, they fail here rather than silently in a frame.
const closureBudget = 800

// closureCallBudget is what a func literal called from more than one place gets
// at each call: twice a method's 80. A range body that costs more than this
// collapses into a walk only through a call the inliner counts as the only one.
const closureCallBudget = 160

// walkShapes is the shape each walk literal inside All()'s literal carries, in
// the order All() declares them. The outer literal carries shape 2 itself, in
// its own body; every other shape a walk is written out for is a literal of its
// own, called once from the outer one.
var walkShapes = []int{1, 3}

// inlinedShapes is every shape a release build runs with no per-Entity call.
func inlinedShapes() []int { return append([]int{2}, walkShapes...) }

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
			"-gcflags=github.com/dvoyni/cog/bundles/ecs/internal=-m=2",
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

// queryMethod splits a Query method's instantiated name into the Query's type
// argument and what follows it — "All", "All.func1", "All.1.2" — and reports
// false for anything that is not a Query's.
func queryMethod(name string) (typeArgument, method string, ok bool) {
	start := strings.Index(name, "(*Query[")
	end := strings.LastIndex(name, "]).")
	if start < 0 || end < start {
		return "", "", false
	}
	return name[start+len("(*Query[") : end], name[end+len("])."):], true
}

// allLiteral names the literals All() returns: the outer one is All.func1, or
// All.1 once an inlining has cloned it, and the nth walk inside it is .funcn or
// .n after either.
var allLiteral = regexp.MustCompile(`^All\.(?:func)?\d+(?:\.(?:func)?(\d+))?$`)

// literalShape reports whether a function name is one of the literals All()
// returns for a Query, as against the one List or Hooks returns, and the shape
// that literal walks.
func literalShape(name string) (shape int, ok bool) {
	_, method, ok := queryMethod(name)
	if !ok {
		return 0, false
	}
	match := allLiteral.FindStringSubmatch(method)
	if match == nil {
		return 0, false
	}
	if match[1] == "" {
		return 2, true
	}
	walk, _ := strconv.Atoi(match[1])
	if walk < 1 || walk > len(walkShapes) {
		// A walk this test does not know of is a shape nobody listed in
		// walkShapes, which is a failure of its own; -1 keeps it reported.
		return -1, true
	}
	return walkShapes[walk-1], true
}

// isAll reports whether a function name is All() itself on some Query.
func isAll(name string) bool {
	_, method, ok := queryMethod(name)
	return ok && method == "All"
}

// queryWidth counts the fields of the Query an instantiated name is for, which
// the name spells out in full: go.shape.struct { A *a; B b; _ Without[c] }.
// It reports -1 for a type argument that is not a struct literal.
func queryWidth(name string) int {
	typeArgument, _, ok := queryMethod(name)
	body, found := strings.CutPrefix(typeArgument, "go.shape.struct {")
	if !ok || !found || !strings.HasSuffix(body, "}") {
		return -1
	}
	body = strings.TrimSpace(strings.TrimSuffix(body, "}"))
	if body == "" {
		return 0
	}
	fields, depth := 1, 0
	for _, r := range body {
		switch r {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ';':
			if depth == 0 {
				fields++
			}
		}
	}
	return fields
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
	typesPosition, rest := match[1], match[2]
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
		return typesPosition, accepted, rest[:cut], rest[cut+len(" with cost "):], true
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
		return typesPosition, refused, rest[:cut], rest[cut+len(": "):], true
	case strings.HasPrefix(rest, inlined):
		return typesPosition, inlined, strings.TrimPrefix(rest, inlined), "", true
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

// rangeBody reports whether a function name is a range statement's own yield
// closure, which the compiler names <caller>-range<n>.
var rangeBody = regexp.MustCompile(`-range\d+$`)

// querySite is one range statement over a Query, as the inliner left it. Every
// verdict about it carries the range statement's line: the call to All() at
// its column, and the literals, the walks and the range body at the for's.
type querySite struct {
	width     int
	all       bool
	literals  map[int]int // shape → inlined walks of that shape
	collapsed int         // range bodies inlined into a walk
}

// walks is how many walks the site carries inlined, and so how many places the
// range body could collapse into.
func (s *querySite) walks() int {
	n := 0
	for _, count := range s.literals {
		n += count
	}
	return n
}

// querySites groups the inliner's verdicts by the range statement they are
// about, keyed by file and line.
func querySites(t *testing.T) map[string]*querySite {
	t.Helper()
	sites := map[string]*querySite{}
	collapses := map[string]int{}
	for line := range strings.SplitSeq(inlineVerdicts(t), "\n") {
		typesPosition, sentence, name, _, ok := inlineSentence(line)
		if !ok || sentence != inlined {
			continue
		}
		at := typesPosition[:strings.LastIndex(typesPosition, ":")]
		if strings.HasPrefix(at, "./query.go:") {
			// All()'s own instantiations and the outer literal compiled as a
			// body of its own, which is where the walks inline when nothing
			// inlines the outer literal: neither is a range statement.
			continue
		}
		if rangeBody.MatchString(name) {
			collapses[at]++
			continue
		}
		shape, literal := literalShape(name)
		if !literal && !isAll(name) {
			continue
		}
		site := sites[at]
		if site == nil {
			site = &querySite{width: -1, literals: map[int]int{}}
			sites[at] = site
		}
		if literal {
			site.literals[shape]++
		} else {
			site.all, site.width = true, queryWidth(name)
		}
	}
	for at, site := range sites {
		site.collapsed = collapses[at]
	}
	return sites
}

// TestAllsFillerStaysInsideTheClosureBudget is the net under the arrangement
// itself: every instantiation of every literal All() returns — the outer one
// carrying shape 2, and each walk literal inside it — must still inline. A
// refusal here is the 2.3x regression, and the cost it reports is how far over
// the edit went.
func TestAllsFillerStaysInsideTheClosureBudget(t *testing.T) {
	type tally struct{ seen, worst int }
	shapes := map[int]*tally{}
	refusals := 0
	for line := range strings.SplitSeq(inlineVerdicts(t), "\n") {
		typesPosition, sentence, name, tail, ok := inlineSentence(line)
		if !ok {
			continue
		}
		shape, literal := literalShape(name)
		if !literal {
			continue
		}
		if shape < 0 {
			t.Errorf("%s: All() carries a walk literal walkShapes does not list: %s", typesPosition, name)
			continue
		}
		switch sentence {
		case refused:
			refusals++
			cost, budget := costAndBudget(tail)
			t.Errorf("%s: All()'s shape-%d literal no longer inlines: cost %s exceeds budget %s\n"+
				"A walk in All() has outgrown the called-once closure budget. Every range site\n"+
				"over this Query shape now pays an indirect call an Entity and loses the row\n"+
				"and fill inlining besides, measured at +4.8 ns an Entity. Move a body back out,\n"+
				"or into a literal of its own. The shape is %s",
				typesPosition, shape, cost, budget, name)
		case accepted:
			cost, err := strconv.Atoi(strings.TrimSpace(tail))
			if err != nil {
				t.Fatalf("%s: unreadable cost %q", typesPosition, tail)
			}
			if shapes[shape] == nil {
				shapes[shape] = &tally{}
			}
			shapes[shape].seen++
			shapes[shape].worst = max(shapes[shape].worst, cost)
			if cost > closureBudget {
				t.Errorf("%s: All()'s shape-%d literal costs %d against a budget of %d", typesPosition, shape, cost, closureBudget)
			}
		}
	}
	// Nothing accepted and nothing refused means the output was not what this
	// test thinks it is, which is a different failure from a blown budget and
	// has to read as one. Refusals alone have already been reported above.
	if len(shapes) == 0 && refusals == 0 {
		t.Fatal("no literal All() returns appeared in the compiler's output, so this test proved nothing")
	}
	for _, shape := range inlinedShapes() {
		counted := shapes[shape]
		if counted == nil {
			if refusals == 0 {
				t.Errorf("no shape-%d literal appeared in the compiler's output: All() no longer writes that walk out", shape)
			}
			continue
		}
		t.Logf("shape %d: %d instantiations, worst %d of %d (%d units of headroom)",
			shape, counted.seen, counted.worst, closureBudget, closureBudget-counted.worst)
	}
}

// TestAllItselfInlinesAtEveryQueryRangeSite is the net under the choice among
// the walks. All() picks one by shape, and none of it is worth anything unless
// All() itself inlines at the range site: the range statement then calls a
// literal it can see, and every walk inside it comes along. All()'s own budget
// is a method's 80, and the literal it returns does not count against it.
func TestAllItselfInlinesAtEveryQueryRangeSite(t *testing.T) {
	worst := 0
	for line := range strings.SplitSeq(inlineVerdicts(t), "\n") {
		typesPosition, sentence, name, tail, ok := inlineSentence(line)
		if !ok || !isAll(name) {
			continue
		}
		switch sentence {
		case refused:
			t.Errorf("%s: All() no longer inlines: %s\n"+
				"Every range site over this Query now calls a func value it cannot see into, so no\n"+
				"walk inlines anywhere, width 2 included. The Query is %s", typesPosition, tail, name)
		case accepted:
			cost, err := strconv.Atoi(strings.TrimSpace(tail))
			if err != nil {
				t.Fatalf("%s: unreadable cost %q", typesPosition, tail)
			}
			worst = max(worst, cost)
		}
	}
	sites := querySites(t)
	if len(sites) == 0 {
		t.Fatal("no range site over a Query appeared in the compiler's output, so this test proved nothing")
	}
	for at, site := range sites {
		if !site.all {
			t.Errorf("%s: a literal All() returns inlined here, but All() itself did not", at)
			continue
		}
		for _, shape := range inlinedShapes() {
			if site.literals[shape] == 0 {
				t.Errorf("%s: All() inlined but its shape-%d walk did not, so a shape-%d Query here pays an indirect call an Entity", at, shape, shape)
			}
		}
	}
	t.Logf("All() inlined at %d Query range sites, worst cost %d of 80", len(sites), worst)
}

// TestAShapeTwoRangeBodyInlinesIntoTheWalk is the net under the win. Inlining
// the literal is necessary but not sufficient: it already inlined before #422,
// at cost 64, while delegating to a filler that did not. What the walk inside
// it buys is the step after — the compiler goes on to inline the range
// statement's own yield closure into the walk, so the loop runs with no
// per-Entity call at all.
//
// That second step is the whole 0.89 ns an Entity, and it is visible only as an
// "inlining call to <caller>-range1" at the same position as the literal's own.
func TestAShapeTwoRangeBodyInlinesIntoTheWalk(t *testing.T) {
	sites := querySites(t)
	queried, collapsed := 0, []string{}
	for at, site := range sites {
		if site.literals[2] == 0 {
			continue
		}
		queried++
		if site.collapsed > 0 {
			collapsed = append(collapsed, at)
		}
	}
	if queried == 0 {
		t.Fatal("no range site inlined a Query's All() literal.\n" +
			"If TestAllsFillerStaysInsideTheClosureBudget also failed, that one names the cause:\n" +
			"a literal over budget inlines nowhere, so no site can collapse its body either.")
	}
	if len(collapsed) == 0 {
		t.Errorf("no range site collapsed its body into the walk, across %d site(s) that inlined a Query's All() literal.\n"+
			"Every shape-2 site should report an \"inlining call to <caller>-range1\" beside the literal's own.\n"+
			"Without it the yield is an indirect call an Entity and the walk is back to what it cost before.",
			queried)
		return
	}
	slices.Sort(collapsed)
	t.Logf("%d of %d Query range sites collapsed a body into a walk: %s",
		len(collapsed), queried, strings.Join(collapsed, ", "))
}

// TestEveryInlinedShapeCollapsesARangeBodyItRuns ties the collapse to the walk
// that runs. Every site carries every walk, and which one runs is q.shape's to
// say at run time, so a collapse reported at a site proves nothing about its
// own shape by itself: before #546 every site reported one, width 1 and 3
// included, into a width-2 walk they never ran.
//
// A site whose range body collapsed into every walk it carries has collapsed
// into its own shape's, whatever that is. So for each inlined shape, a site
// over a Query of that width has to report as many collapses as walks.
func TestEveryInlinedShapeCollapsesARangeBodyItRuns(t *testing.T) {
	sites := querySites(t)
	for _, shape := range inlinedShapes() {
		var full, partial []string
		for at, site := range sites {
			if site.width != shape {
				continue
			}
			if walks := site.walks(); walks > 0 && site.collapsed >= walks {
				full = append(full, at)
			} else {
				partial = append(partial, fmt.Sprintf("%s (%d of %d)", at, site.collapsed, walks))
			}
		}
		if len(full) == 0 {
			slices.Sort(partial)
			t.Errorf("no width-%d range site collapsed its body into every walk it carries, so none is known to collapse into\n"+
				"the shape-%d walk it runs. Width-%d sites, with collapses of walks: %s",
				shape, shape, shape, strings.Join(partial, ", "))
			continue
		}
		slices.Sort(full)
		t.Logf("width %d: %d site(s) collapse into every walk, among them %s", shape, len(full), full[0])
	}
}

// heavyWidthTwoWalk is a width-2 range site whose body costs more than
// closureCallBudget, which TestAWidthTwoBodyPastTheCallBudgetStillCollapses
// reads. What it does is beside the point, but it runs in that test, so it has
// to be something.
func heavyWidthTwoWalk(q *Query[width2]) (sum float32) {
	for e, it := range q.All() {
		x, y := it.W1.X, it.W1.Y
		switch e.idx() % 4 {
		case 0:
			sum += x*y + 1
		case 1:
			sum -= x*x - y
		case 2:
			sum += y*y*x - x
		default:
			sum -= x + y*2
		}
		if sum > 1e6 {
			sum = sum/2 + x
		} else if sum < -1e6 {
			sum = sum/3 - y
		}
		it.W0.X, it.W0.Y = it.W0.X+x*0.5, it.W0.Y-y*0.25
		if it.W0.X > 100 {
			it.W0.X = 0
		}
		if it.W0.Y < -100 {
			it.W0.Y = 0
		}
		switch {
		case x > y:
			it.W0.X, it.W0.Y = it.W0.Y, it.W0.X
		case x < y:
			it.W0.X -= y - x
		default:
			it.W0.Y += x * y
		}
	}
	return sum
}

// TestAWidthTwoBodyPastTheCallBudgetStillCollapses is the net under the order
// of the walks. With more than one walk inline, a range body is called from as
// many places as there are walks, and the inliner gives the 800 only to a call
// it counts as the only one: the first it resolves. Every later call gets
// closureCallBudget, 160. The width-2 walk is written in the outer literal's
// own body so that its call is that first one, and a width-2 body between 160
// and 800 keeps collapsing as it did before any other walk existed.
//
// Moved into a literal of its own beside the others, the width-2 walk's call
// is resolved in the same batch as theirs, and a body like this one's, or
// scene's record-range3, stops collapsing at width 2 as well.
func TestAWidthTwoBodyPastTheCallBudgetStillCollapses(t *testing.T) {
	const body = "heavyWidthTwoWalk-range1"
	cost, collapsed := -1, false
	for line := range strings.SplitSeq(inlineVerdicts(t), "\n") {
		_, sentence, name, tail, ok := inlineSentence(line)
		if !ok || name != body {
			continue
		}
		switch sentence {
		case accepted:
			cost, _ = strconv.Atoi(strings.TrimSpace(tail))
		case inlined:
			collapsed = true
		}
	}
	if cost <= closureCallBudget {
		t.Fatalf("%s costs %d, which is not past %d, so this fixture proves nothing: make its body heavier", body, cost, closureCallBudget)
	}
	if !collapsed {
		t.Errorf("%s, at cost %d, no longer collapses into the width-2 walk.\n"+
			"A width-2 range body past %d collapses only through the call the inliner counts as the only one,\n"+
			"and that is the width-2 walk's only while it sits in the outer literal's own body.", body, cost, closureCallBudget)
	} else {
		t.Logf("%s, at cost %d of %d, collapses into the width-2 walk", body, cost, closureBudget)
	}
	_, q := widthWorld[width2](t, 8)
	heavyWidthTwoWalk(q)
}
