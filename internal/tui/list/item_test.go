package list

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func validItem(id string) Item {
	return Item{ID: ItemID{ID: id}, Primary: id, SearchFields: []SearchField{{Tier: SearchPrimary, Text: id}}}
}

func TestValidateRejectsInvalidItemTrees(t *testing.T) {
	tooMany := make([]Item, MaxItems+1)
	for i := range tooMany {
		tooMany[i] = validItem(fmt.Sprintf("item-%d", i))
	}
	depthFour := validItem("one")
	depthFour.Children = []Item{validItem("two")}
	depthFour.Children[0].Children = []Item{validItem("three")}
	depthFour.Children[0].Children[0].Children = []Item{validItem("four")}

	for _, test := range []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{name: "empty ID", snapshot: Snapshot{Items: []Item{{Primary: "empty"}}}, want: "empty ID"},
		{name: "ancestry without local ID", snapshot: Snapshot{Items: []Item{{ID: ItemID{Scope: "scope", Parent: "parent"}, Primary: "empty"}}}, want: "empty ID"},
		{name: "duplicate composite ID", snapshot: Snapshot{Items: []Item{
			{ID: ItemID{Scope: "scope", Parent: "parent", ID: "same"}, Primary: "first"},
			{ID: ItemID{Scope: "scope", Parent: "parent", ID: "same"}, Primary: "second"},
		}}, want: "duplicate item ID"},
		{name: "duplicate ID", snapshot: Snapshot{Items: []Item{validItem("same"), validItem("same")}}, want: "duplicate item ID"},
		{name: "empty primary", snapshot: Snapshot{Items: []Item{{ID: ItemID{ID: "item"}}}}, want: "empty primary text"},
		{name: "unknown search tier", snapshot: Snapshot{Items: []Item{{ID: ItemID{ID: "item"}, Primary: "item", SearchFields: []SearchField{{Tier: 9, Text: "item"}}}}}, want: "invalid tier"},
		{name: "unordered search tiers", snapshot: Snapshot{Items: []Item{{ID: ItemID{ID: "item"}, Primary: "item", SearchFields: []SearchField{{Tier: SearchSecondary, Text: "path"}, {Tier: SearchPrimary, Text: "name"}}}}}, want: "not ordered"},
		{name: "empty search field", snapshot: Snapshot{Items: []Item{{ID: ItemID{ID: "item"}, Primary: "item", SearchFields: []SearchField{{Tier: SearchPrimary}}}}}, want: "search field 0 is empty"},
		{name: "stale active ID", snapshot: Snapshot{Items: []Item{validItem("item")}, ActiveItemID: ItemID{ID: "missing"}}, want: "active item ID"},
		{name: "excessive depth", snapshot: Snapshot{Items: []Item{depthFour}}, want: "maximum depth"},
		{name: "excessive count", snapshot: Snapshot{Items: tooMany}, want: "maximum count"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, Validate(test.snapshot), test.want)
		})
	}
}

func TestValidatePreservesOpaqueLocalIDs(t *testing.T) {
	opaque := validItem(" \t ")
	opaque.ID.Scope = " scope:/\t "
	opaque.ID.Parent = " parent:/\t "
	opaque.Primary = "visible label"
	opaque.SearchFields = nil

	err := Validate(Snapshot{Items: []Item{opaque}, ActiveItemID: opaque.ID})

	require.NoError(t, err)
}

func TestValidateAcceptsEmptyFlatAndThreeLevelTrees(t *testing.T) {
	require.NoError(t, Validate(Snapshot{}))
	root := validItem("root")
	root.Children = []Item{validItem("child")}
	root.Children[0].Children = []Item{validItem("grandchild")}
	require.NoError(t, Validate(Snapshot{Items: []Item{root}, ActiveItemID: ItemID{ID: "grandchild"}}))
}
