// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import "sort"

type viewportSelection struct {
	index int
	count int64
}

// Only selection data is retained in Go. Payload columns stay in DuckDB.
// Parallel numeric arrays avoid per-event pointers and repeated track/row data.
// Events are ordered by start time. All three event arrays use the same index.
// Each tree stores event indices, not timestamps or event IDs.
// For n events, internal nodes occupy [1, n); index zero is unused.
// Leaves [n, 2*n) are implicit: leaf n+i refers to event i.
// The children of internal node k are 2*k and 2*k+1. No padding is used.
// durationWinners selects the longest event, then the lowest event index.
// latestEndWinners selects an event with the latest end time for subtree pruning.
type viewportLane struct {
	events, starts, ends              []int64
	durationWinners, latestEndWinners []int
}

func newViewportLane(events, starts, ends []int64) *viewportLane {
	l := &viewportLane{
		events:           events,
		starts:           starts,
		ends:             ends,
		durationWinners:  make([]int, len(events)),
		latestEndWinners: make([]int, len(events)),
	}
	for node := len(events) - 1; node > 0; node-- {
		l.durationWinners[node] = l.longerEvent(l.eventIndexAtNode(l.durationWinners, node*2), l.eventIndexAtNode(l.durationWinners, node*2+1))
		a, b := l.eventIndexAtNode(l.latestEndWinners, node*2), l.eventIndexAtNode(l.latestEndWinners, node*2+1)
		if ends[a] >= ends[b] {
			l.latestEndWinners[node] = a
		} else {
			l.latestEndWinners[node] = b
		}
	}
	return l
}

// eventIndexAtNode resolves a stored internal node or an implicit leaf.
func (l *viewportLane) eventIndexAtNode(tree []int, node int) int {
	if node >= len(l.events) {
		return node - len(l.events)
	}
	return tree[node]
}

// longerEvent selects the longer event, then the lower index on a tie.
// A negative first index means that no event has been selected yet.
func (l *viewportLane) longerEvent(a, b int) int {
	if a < 0 {
		return b
	}
	// Unsigned subtraction represents every positive int64 timestamp distance,
	// including durations crossing zero that are larger than MaxInt64.
	ad := uint64(l.ends[a]) - uint64(l.starts[a]) // #nosec G115 -- validated end > start; unsigned subtraction preserves the full timestamp distance.
	bd := uint64(l.ends[b]) - uint64(l.starts[b]) // #nosec G115 -- validated end > start; unsigned subtraction preserves the full timestamp distance.
	if ad > bd || ad == bd && a < b {
		return a
	}
	return b
}

// query returns each event that starts before start and ends after start,
// then one longest event per occupied bucket in [start, end). step must be positive.
func (l *viewportLane) query(start, end, step int64) []viewportSelection {
	if len(l.events) == 0 || end <= start {
		return nil
	}
	position := l.firstEventStartingAtOrAfter(start)
	var result []viewportSelection
	l.appendOverlappingEvents(&result, position, start)
	l.appendBucketSelections(&result, position, start, end, step)
	return result
}

func (l *viewportLane) firstEventStartingAtOrAfter(time int64) int {
	return sort.Search(len(l.events), func(i int) bool { return l.starts[i] >= time })
}

// appendOverlappingEvents appends events in [0, position) that end after start.
// position is the first event that starts at or after start.
func (l *viewportLane) appendOverlappingEvents(result *[]viewportSelection, position int, start int64) {
	// Skip subtrees whose latest end is at or before the viewport start.
	var appendOverlappingSubtree func(int)
	appendOverlappingSubtree = func(node int) {
		if l.ends[l.eventIndexAtNode(l.latestEndWinners, node)] <= start {
			return
		}
		if node >= len(l.events) {
			*result = append(*result, viewportSelection{node - len(l.events), 1})
		} else {
			appendOverlappingSubtree(node * 2)
			appendOverlappingSubtree(node*2 + 1)
		}
	}
	// Cover the event-index range [0, position) with tree nodes.
	// Process exposed boundary nodes before moving to their parents.
	for left, right := len(l.events), len(l.events)+position; left < right; left, right = left/2, right/2 {
		if left%2 != 0 {
			appendOverlappingSubtree(left)
			left++
		}
		if right%2 != 0 {
			right--
			appendOverlappingSubtree(right)
		}
	}
	// Range traversal does not emit events in index order.
	sort.Slice(*result, func(i, j int) bool { return (*result)[i].index < (*result)[j].index })
}

// appendBucketSelections appends one longest event per occupied bucket.
// Each selection counts all events that start in that bucket.
func (l *viewportLane) appendBucketSelections(result *[]viewportSelection, position int, start, end, step int64) {
	for position < len(l.events) && l.starts[position] < end {
		time := l.starts[position]
		boundary := viewportBucketEnd(time, start, end, step)
		next := l.firstEventStartingAtOrAfter(boundary)
		// Select the longest event in the bucket range [position, next).
		// Keep this traversal here to avoid a helper call for each bucket.
		selected := -1
		for left, right := position+len(l.events), next+len(l.events); left < right; left, right = left/2, right/2 {
			if left%2 != 0 {
				selected = l.longerEvent(selected, l.eventIndexAtNode(l.durationWinners, left))
				left++
			}
			if right%2 != 0 {
				right--
				selected = l.longerEvent(selected, l.eventIndexAtNode(l.durationWinners, right))
			}
		}
		*result = append(*result, viewportSelection{selected, int64(next - position)})
		position = next
	}
}

// viewportBucketEnd returns the exclusive end of the bucket that contains time,
// capped at end. Buckets are aligned to start. start <= time < end and step > 0.
func viewportBucketEnd(time, start, end, step int64) int64 {
	delta := uint64(time) - uint64(start)          // #nosec G115 -- time >= start; unsigned subtraction preserves distances above MaxInt64.
	remaining := uint64(step) - delta%uint64(step) // #nosec G115 -- request validation guarantees step > 0.
	boundary := end
	if remaining < uint64(end)-uint64(time) { // #nosec G115 -- time < end; unsigned subtraction preserves the full timestamp distance.
		boundary = int64(uint64(time) + remaining) // #nosec G115 -- modular addition yields a signed timestamp strictly between time and end.
	}
	return boundary
}
