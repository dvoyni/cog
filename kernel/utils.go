package kernel

// ordered is implemented by items that can be topologically sorted by their
// Before/After constraints. ID must uniquely identify an item within the set.
type ordered[ID comparable] interface {
	orderID() ID
	orderBefore() []ID
	orderAfter() []ID
}

// sortTopologically orders items so that every Before/After constraint is honored, using
// Kahn's algorithm with a deterministic tie-break (lowest original index first).
// Before/After references to IDs not present in items impose no constraint and
// are ignored. If the constraints are unsatisfiable, sorted is nil and cycle
// holds the IDs of the items involved in the cycle; otherwise cycle is nil.
//
// An optional wildcard ID may be given. An item listing the wildcard in its
// After runs after every other item (i.e. last); listing it in Before runs
// before every other item (i.e. first). Order among several such "last" (or
// "first") items is unconstrained relative to each other.
func sortTopologically[ID comparable, T ordered[ID]](items []T, wildcards ...ID) (sorted []T, cycle []ID) {
	n := len(items)
	index := make(map[ID]int, n)
	for i, item := range items {
		index[item.orderID()] = i
	}

	var wildcard ID
	hasWildcard := len(wildcards) > 0
	if hasWildcard {
		wildcard = wildcards[0]
	}

	adj := make([][]int, n) // edge from -> to means "from" runs before "to"
	indeg := make([]int, n)
	type edge struct{ from, to int }
	seen := make(map[edge]struct{})

	addEdge := func(from, to int) {
		if from == to {
			return
		}
		e := edge{from, to}
		if _, dup := seen[e]; dup {
			return
		}
		seen[e] = struct{}{}
		adj[from] = append(adj[from], to)
		indeg[to]++
	}

	first := make([]bool, n) // Before contains the wildcard: run first
	last := make([]bool, n)  // After contains the wildcard: run last

	for i, item := range items {
		// After: item must run after X => X -> item
		for _, id := range item.orderAfter() {
			if hasWildcard && id == wildcard {
				last[i] = true
				continue
			}
			if j, ok := index[id]; ok {
				addEdge(j, i)
			}
		}
		// Before: item must run before Y => item -> Y
		for _, id := range item.orderBefore() {
			if hasWildcard && id == wildcard {
				first[i] = true
				continue
			}
			if j, ok := index[id]; ok {
				addEdge(i, j)
			}
		}
	}

	// Wildcard edges: "last" items run after every non-last item; "first" items
	// run before every non-first item.
	if hasWildcard {
		for i := 0; i < n; i++ {
			if last[i] {
				for j := 0; j < n; j++ {
					if !last[j] {
						addEdge(j, i)
					}
				}
			}
			if first[i] {
				for j := 0; j < n; j++ {
					if !first[j] {
						addEdge(i, j)
					}
				}
			}
		}
	}

	result := make([]T, 0, n)
	done := make([]bool, n)
	for len(result) < n {
		next := -1
		for i := 0; i < n; i++ {
			if !done[i] && indeg[i] == 0 {
				next = i
				break
			}
		}
		if next == -1 {
			remaining := make([]ID, 0)
			for i := 0; i < n; i++ {
				if !done[i] {
					remaining = append(remaining, items[i].orderID())
				}
			}
			return nil, remaining
		}
		done[next] = true
		indeg[next] = -1
		result = append(result, items[next])
		for _, to := range adj[next] {
			indeg[to]--
		}
	}
	return result, nil
}
