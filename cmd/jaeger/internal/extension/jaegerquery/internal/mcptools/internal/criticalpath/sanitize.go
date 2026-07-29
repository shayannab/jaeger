// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package criticalpath

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// removeOverflowingChildren removes or adjusts child spans that overflow their parent's time range.
// An overflowing child span is one whose time range falls outside its parent span's time range.
// The function adjusts the start time and duration of overflowing child spans
// to ensure they fit within the time range of their parent span.
func removeOverflowingChildren(spanMap map[pcommon.SpanID]CPSpan) map[pcommon.SpanID]CPSpan {
	// First pass: drop all spans whose parent is not empty and missing from map,
	// along with all their descendants.
	var orphans []pcommon.SpanID
	for spanID, span := range spanMap {
		if !span.ParentSpanID.IsEmpty() {
			if _, parentExists := spanMap[span.ParentSpanID]; !parentExists {
				orphans = append(orphans, spanID)
			}
		}
	}

	for _, orphanID := range orphans {
		if span, exists := spanMap[orphanID]; exists {
			delete(spanMap, orphanID)
			dropDescendants(spanMap, span)
		}
	}

	// Second pass: find real roots (ParentSpanID is empty) and sanitize top-down.
	var roots []pcommon.SpanID
	for spanID, span := range spanMap {
		if span.ParentSpanID.IsEmpty() {
			roots = append(roots, spanID)
		}
	}

	for _, rootID := range roots {
		sanitizeSpan(spanMap, rootID)
	}

	return spanMap
}

func sanitizeSpan(spanMap map[pcommon.SpanID]CPSpan, spanID pcommon.SpanID) {
	span, ok := spanMap[spanID]
	if !ok {
		return
	}

	filteredChildren := make([]pcommon.SpanID, 0, len(span.ChildSpanIDs))
	for _, childID := range span.ChildSpanIDs {
		child, childExists := spanMap[childID]
		if !childExists {
			continue
		}

		keepChild := true
		childEndTime := child.StartTime + child.Duration
		parentEndTime := span.StartTime + span.Duration

		if child.StartTime >= span.StartTime {
			if child.StartTime >= parentEndTime {
				// child starts at or after parent ends => drop the child span
				//      |----parent----|
				//                        |----child--|
				keepChild = false
			} else if childEndTime > parentEndTime {
				// child ends after parent => truncate duration to fit parent
				//      |----parent----|
				//              |----child--|
				child.Duration = parentEndTime - child.StartTime
			}
		} else {
			// child starts before parent
			if childEndTime <= span.StartTime {
				// child ends at or before parent starts => drop the child span
				//                      |----parent----|
				//       |----child--|
				keepChild = false
			} else if childEndTime <= parentEndTime {
				// child starts before parent, ends before/at parent end => truncate start
				//      |----parent----|
				//   |----child--|
				child.StartTime = span.StartTime
				child.Duration = childEndTime - span.StartTime
			} else {
				// child starts before parent and ends after parent => truncate both
				//      |----parent----|
				//  |---------child---------|
				child.StartTime = span.StartTime
				child.Duration = parentEndTime - span.StartTime
			}
		}

		if keepChild {
			spanMap[childID] = child
			filteredChildren = append(filteredChildren, childID)
			// Recursively sanitize child's subtree
			sanitizeSpan(spanMap, childID)
		} else {
			// Drop child span and all its descendants recursively
			delete(spanMap, childID)
			dropDescendants(spanMap, child)
		}
	}

	span.ChildSpanIDs = filteredChildren
	spanMap[spanID] = span
}

func dropDescendants(spanMap map[pcommon.SpanID]CPSpan, span CPSpan) {
	for _, childID := range span.ChildSpanIDs {
		if child, exists := spanMap[childID]; exists {
			delete(spanMap, childID)
			dropDescendants(spanMap, child)
		}
	}
}
