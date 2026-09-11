package sbench

import "testing"

// Why cog#242 was ruled out of scope rather than answered.
//
// cog#240 made All() walk the driver's dense array backwards *as a guarantee*,
// and that guarantee is the whole reason "you may restructure the Entity you
// are on" is safe under swap-remove: the removal moves the last row into the
// hole, and a walk that started at the end has already been there.
//
// Splitting the walk into sub-ranges -- the one thing chunk-parallel execution
// needs from the iteration contract -- destroys it. The low shard's removals
// drag the high half down into indices that shard has already passed, so those
// entities are visited never and removed never.
//
// Nothing here is concurrent. The shards run one after another, so the failure
// is index arithmetic alone and no data race is involved; adding real
// concurrency could only make it worse.

// splitStore models cog#239's Store: a packed dense array with swap-remove.
type splitStore struct{ dense []int }

func (s *splitStore) removeAt(i int) {
	last := len(s.dense) - 1
	s.dense[i] = s.dense[last]
	s.dense = s.dense[:last]
}

// walkBack walks [lo,hi) backwards, removing every entity it visits -- the case
// cog#240 declares safe when the range is the whole array.
func walkBack(s *splitStore, lo, hi int, seen map[int]bool) {
	for i := hi - 1; i >= lo; i-- {
		if i >= len(s.dense) {
			continue // this shard's range now runs past the end
		}
		seen[s.dense[i]] = true
		s.removeAt(i)
	}
}

func runShards(n, shards int) (visited, leftOver int) {
	s := &splitStore{dense: make([]int, n)}
	for i := range s.dense {
		s.dense[i] = i
	}
	seen := make(map[int]bool, n)
	per := n / shards
	for k := range shards {
		lo, hi := k*per, k*per+per
		if k == shards-1 {
			hi = n
		}
		walkBack(s, lo, hi, seen)
	}
	return len(seen), len(s.dense)
}

func TestWholeRangeReverseWalkVisitsEverything(t *testing.T) {
	visited, left := runShards(1000, 1)
	if visited != 1000 || left != 0 {
		t.Fatalf("cog#240's guarantee does not hold: visited %d/1000, %d left", visited, left)
	}
}

func TestSplitRangeReverseWalkSilentlySkipsHalf(t *testing.T) {
	for _, shards := range []int{2, 4, 8} {
		visited, left := runShards(1000, shards)
		t.Logf("shards=%d visited %d/1000, %d left in store", shards, visited, left)
		if visited == 1000 {
			t.Fatalf("shards=%d unexpectedly visited everything; "+
				"cog#242's premise may be recoverable after all", shards)
		}
	}
}
