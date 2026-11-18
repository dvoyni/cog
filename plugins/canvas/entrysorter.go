package canvas

import "slices"

// sortedEntry is one of a frame's entries in its sort: its sort key, and its
// place among the frame's shapes as merged, lane after lane, which tells its
// draw list's place and its own in that list once sorted.
type sortedEntry struct {
	key        sortKey
	shapeIndex uint32
}

// entrySorter sorts a frame's entries by layer, then order, then the place
// of their draw list, then the order they were recorded in: a radix sort by
// sort key, stable, so the merge's order gives the last two. In a layer it
// regroups, each run of entries of equal keys is then regrouped by batch. It
// keeps its buffers from frame to frame, so a steady frame allocates
// nothing.
type entrySorter struct {
	// regroupedLayers are the layers whose entries of equal keys are
	// regrouped, sorted, each once.
	regroupedLayers []int32
	// entries hold the entries of the frame last sorted, in sort order.
	entries []sortedEntry
	// scratch is what the sort and the regrouping move entries through.
	scratch []sortedEntry
	// digitCounts are the sort's count of each value of each byte of the
	// keys, the lowest byte first.
	digitCounts [8][256]int
}

// makeEntrySorter answers a sorter regrouping regroupedLayers, which it
// copies, in any order and with any repeated.
func makeEntrySorter(regroupedLayers []int32) entrySorter {
	sortedLayers := slices.Clone(regroupedLayers)
	slices.Sort(sortedLayers)
	return entrySorter{regroupedLayers: slices.Compact(sortedLayers)}
}

// sort sorts the entries of shapes, the frame's as merged, into the
// sorter's entries, and answers them, valid until the next sort.
func (s *entrySorter) sort(shapes []Shape) []sortedEntry {
	s.entries = s.entries[:0]
	for index, shape := range shapes {
		s.entries = append(s.entries, sortedEntry{key: makeSortKey(shape.Layer, shape.Order), shapeIndex: uint32(index)})
	}
	s.sortByKey()
	s.regroup(shapes)
	return s.entries
}

// sortByKey sorts the entries by key, stably, one pass of a byte each from
// the lowest, skipping a pass whose byte is alike in every entry, such as
// the high bytes of a few layers.
func (s *entrySorter) sortByKey() {
	if len(s.entries) < 2 {
		return
	}
	s.digitCounts = [8][256]int{}
	for _, entry := range s.entries {
		for digit := range s.digitCounts {
			s.digitCounts[digit][byte(entry.key>>(8*digit))]++
		}
	}
	s.scratch = slices.Grow(s.scratch[:0], len(s.entries))[:len(s.entries)]
	// Each pass moves the entries from source into target, and the two
	// change places after it.
	source := s.entries
	target := s.scratch
	for digit := range s.digitCounts {
		digitShift := 8 * digit
		// The counts of this byte's values become where each value's
		// entries start in target.
		valueStarts := &s.digitCounts[digit]
		if valueStarts[byte(source[0].key>>digitShift)] == len(source) {
			continue
		}
		nextStart := 0
		for digitValue, valueCount := range valueStarts {
			valueStarts[digitValue] = nextStart
			nextStart += valueCount
		}
		for _, entry := range source {
			digitValue := byte(entry.key >> digitShift)
			target[valueStarts[digitValue]] = entry
			valueStarts[digitValue]++
		}
		sorted := target
		target = source
		source = sorted
	}
	s.entries = source
	s.scratch = target
}

// regroup regroups by batch each run of entries of equal keys in a layer
// the sorter regroups.
func (s *entrySorter) regroup(shapes []Shape) {
	if len(s.regroupedLayers) == 0 {
		return
	}
	for runStart := 0; runStart < len(s.entries); {
		runKey := s.entries[runStart].key
		runEnd := runStart + 1
		for runEnd < len(s.entries) && s.entries[runEnd].key == runKey {
			runEnd++
		}
		if runEnd-runStart > 1 {
			if _, regrouped := slices.BinarySearch(s.regroupedLayers, shapes[s.entries[runStart].shapeIndex].Layer); regrouped {
				s.regroupRun(s.entries[runStart:runEnd], shapes)
			}
		}
		runStart = runEnd
	}
}

// regroupRun moves run's entries of its first entry's batch to its front,
// then those of the first batch left after them, and so on, each batch's in
// the order they were in.
func (s *entrySorter) regroupRun(run []sortedEntry, shapes []Shape) {
	for batchStart := 0; batchStart < len(run); {
		firstBatchKey := shapes[run[batchStart].shapeIndex].batchKey()
		s.scratch = s.scratch[:0]
		nextBatchEnd := batchStart
		for _, entry := range run[batchStart:] {
			if shapes[entry.shapeIndex].batchKey() == firstBatchKey {
				run[nextBatchEnd] = entry
				nextBatchEnd++
			} else {
				s.scratch = append(s.scratch, entry)
			}
		}
		copy(run[nextBatchEnd:], s.scratch)
		batchStart = nextBatchEnd
	}
}
