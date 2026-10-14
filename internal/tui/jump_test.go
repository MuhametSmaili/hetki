package tui

import (
	"testing"

	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func jumpIDs(count int) []list.ItemID {
	ids := make([]list.ItemID, count)
	for i := range ids {
		ids[i] = list.ItemID{ID: string(rune('A' + i))}
	}
	return ids
}

func TestAssignJumpLabelsUsesTheExactAlphabetInVisibleOrder(t *testing.T) {
	ids := jumpIDs(len([]rune(jumpAlphabet)))
	labels := assignJumpLabels(ids)
	require.Len(t, labels, len(ids))
	for i, label := range []rune(jumpAlphabet) {
		require.Equal(t, string(label), labels[i].label)
		require.Equal(t, ids[i], labels[i].itemID)
	}
}

func TestAssignJumpLabelsUsesUniqueFixedWidthLabelsBeyondTheAlphabet(t *testing.T) {
	labels := assignJumpLabels(jumpIDs(len([]rune(jumpAlphabet)) + 5))
	seen := make(map[string]bool, len(labels))
	for _, candidate := range labels {
		require.Len(t, []rune(candidate.label), 2)
		require.False(t, seen[candidate.label])
		seen[candidate.label] = true
	}
	for _, left := range labels {
		for _, right := range labels {
			if left.label != right.label {
				require.NotContains(t, right.label, left.label, "fixed-width label %q is a prefix of %q", left.label, right.label)
			}
		}
	}
}

func TestJumpInputHandlesPartialInvalidAndShortcutLabels(t *testing.T) {
	jump := jumpState{candidates: assignJumpLabels(jumpIDs(28))}
	itemID, complete, valid := jump.enter("a")
	require.Empty(t, itemID)
	require.False(t, complete)
	require.True(t, valid)
	itemID, complete, valid = jump.enter("a")
	require.Equal(t, list.ItemID{ID: "A"}, itemID)
	require.True(t, complete)
	require.True(t, valid)

	jump.input = ""
	_, complete, valid = jump.enter("!")
	require.False(t, complete)
	require.False(t, valid)
	require.Empty(t, jump.input)

	single := jumpState{candidates: assignJumpLabels(jumpIDs(12))}
	itemID, complete, valid = single.enter("q")
	require.Equal(t, jump.candidates[10].itemID, itemID)
	require.True(t, complete)
	require.True(t, valid)
}
