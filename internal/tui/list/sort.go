package list

import (
	"context"
	"slices"
)

// Sort bounded runs, then merge with cancellation checkpoints. Taking the left
// entry on ties preserves the original sibling order through every merge.
func sortMatches(ctx context.Context, items []matchedItem) {
	if matchesOrdered(ctx, items) {
		return
	}
	const run = 64
	for start := 0; start < len(items); start += run {
		if ctx.Err() != nil {
			return
		}
		slices.SortStableFunc(items[start:min(start+run, len(items))], func(a, b matchedItem) int {
			return b.rank - a.rank
		})
	}
	if len(items) <= run || ctx.Err() != nil {
		return
	}
	scratch := make([]matchedItem, len(items))
	for width := run; width < len(items); width *= 2 {
		for start := 0; start < len(items); start += 2 * width {
			end := min(start+2*width, len(items))
			middle := min(start+width, end)
			if !mergeMatches(ctx, scratch[start:end], items[start:middle], items[middle:end]) {
				return
			}
		}
		copy(items, scratch)
	}
}

func matchesOrdered(ctx context.Context, items []matchedItem) bool {
	for i := 1; i < len(items); i++ {
		if i%64 == 0 && ctx.Err() != nil {
			return false
		}
		if items[i-1].rank < items[i].rank {
			return false
		}
	}
	return true
}

func mergeMatches(ctx context.Context, dst, left, right []matchedItem) bool {
	l, r := 0, 0
	for i := range dst {
		if i%64 == 0 && ctx.Err() != nil {
			return false
		}
		if r == len(right) || (l < len(left) && left[l].rank >= right[r].rank) {
			dst[i] = left[l]
			l++
		} else {
			dst[i] = right[r]
			r++
		}
	}
	return true
}
