package list

import (
	"fmt"
	"strings"
)

const (
	MaxItems = 10_000
	MaxDepth = 3
)

// ItemID is a comparable row key. Scope and Parent are opaque ancestry;
// ID is the required local identity. No component is normalized.
type ItemID struct {
	Scope  string
	Parent string
	ID     string
}

// IsZero reports whether the key is absent, not whether it is valid.
func (id ItemID) IsZero() bool { return id == (ItemID{}) }

type SearchTier uint8

const (
	SearchPrimary SearchTier = iota + 1
	SearchSecondary
)

type SearchField struct {
	Tier SearchTier
	Text string
}

type Item struct {
	ID           ItemID
	Primary      string
	Secondary    string
	Trailing     string // Optional right-aligned text, independent of search fields.
	SearchFields []SearchField
	Children     []Item
}

type Snapshot struct {
	Items        []Item
	ActiveItemID ItemID
	Notice       string
}

func Validate(snapshot Snapshot) error {
	ids := make(map[ItemID]struct{})
	if err := validateItems(snapshot.Items, 1, ids); err != nil {
		return err
	}
	if !snapshot.ActiveItemID.IsZero() {
		if _, exists := ids[snapshot.ActiveItemID]; !exists {
			return fmt.Errorf("active item ID %q is not present", snapshot.ActiveItemID)
		}
	}
	return nil
}

func validateItems(items []Item, depth int, ids map[ItemID]struct{}) error {
	if len(items) == 0 {
		return nil
	}
	if depth > MaxDepth {
		return fmt.Errorf("item tree exceeds maximum depth %d", MaxDepth)
	}
	for i, item := range items {
		if len(ids) >= MaxItems {
			return fmt.Errorf("item tree exceeds maximum count %d", MaxItems)
		}
		if item.ID.ID == "" {
			return fmt.Errorf("item at depth %d index %d has an empty ID", depth, i)
		}
		if _, exists := ids[item.ID]; exists {
			return fmt.Errorf("duplicate item ID %q", item.ID)
		}
		ids[item.ID] = struct{}{}
		if err := validateItem(item); err != nil {
			return err
		}
		if err := validateItems(item.Children, depth+1, ids); err != nil {
			return err
		}
	}
	return nil
}

func validateItem(item Item) error {
	if strings.TrimSpace(item.Primary) == "" {
		return fmt.Errorf("item %q has empty primary text", item.ID)
	}
	previousTier := SearchPrimary
	for i, field := range item.SearchFields {
		if field.Tier != SearchPrimary && field.Tier != SearchSecondary {
			return fmt.Errorf("item %q search field %d has invalid tier %d", item.ID, i, field.Tier)
		}
		if i > 0 && field.Tier < previousTier {
			return fmt.Errorf("item %q search fields are not ordered by tier", item.ID)
		}
		if strings.TrimSpace(field.Text) == "" {
			return fmt.Errorf("item %q search field %d is empty", item.ID, i)
		}
		previousTier = field.Tier
	}
	return nil
}

func IDs(items []Item) map[ItemID]struct{} {
	ids := make(map[ItemID]struct{})
	var collect func([]Item)
	collect = func(items []Item) {
		for _, item := range items {
			ids[item.ID] = struct{}{}
			collect(item.Children)
		}
	}
	collect(items)
	return ids
}
