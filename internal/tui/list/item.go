package list

import (
	"context"
	"fmt"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/search"
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

type SearchTier = search.Tier

const (
	SearchPrimary   = search.Primary
	SearchSecondary = search.Secondary
)

type SearchField = search.Field

type Item struct {
	ID           ItemID
	Context      string // Optional leading column; enables column layout.
	Primary      string
	Annotation   string // Optional muted column following Primary.
	Secondary    string
	Trailing     string // Optional trailing text, independent of search fields.
	SearchFields []SearchField
	Children     []Item
}

type Snapshot struct {
	Items        []Item
	ActiveItemID ItemID
	Notice       string
}

func Validate(snapshot Snapshot) error {
	_, err := validateSnapshot(context.Background(), snapshot)
	return err
}

func validateSnapshot(ctx context.Context, snapshot Snapshot) (map[ItemID]struct{}, error) {
	ids := make(map[ItemID]struct{})
	if err := validateItems(ctx, snapshot.Items, 1, ids); err != nil {
		return nil, err
	}
	if !snapshot.ActiveItemID.IsZero() {
		if _, exists := ids[snapshot.ActiveItemID]; !exists {
			return nil, fmt.Errorf("active item ID %q is not present", snapshot.ActiveItemID)
		}
	}
	return ids, ctx.Err()
}

func validateItems(ctx context.Context, items []Item, depth int, ids map[ItemID]struct{}) error {
	if len(items) == 0 {
		return nil
	}
	if depth > MaxDepth {
		return fmt.Errorf("item tree exceeds maximum depth %d", MaxDepth)
	}
	for i, item := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		if item.ID.ID == "" {
			return fmt.Errorf("item at depth %d index %d has an empty ID", depth, i)
		}
		if err := registerItem(item, ids); err != nil {
			return err
		}
		if err := validateItems(ctx, item.Children, depth+1, ids); err != nil {
			return err
		}
	}
	return nil
}

func registerItem(item Item, ids map[ItemID]struct{}) error {
	if len(ids) >= MaxItems {
		return fmt.Errorf("item tree exceeds maximum count %d", MaxItems)
	}
	if _, exists := ids[item.ID]; exists {
		return fmt.Errorf("duplicate item ID %q", item.ID)
	}
	ids[item.ID] = struct{}{}
	return validateItem(item)
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
	return itemIDs(context.Background(), items)
}

func itemIDs(ctx context.Context, items []Item) map[ItemID]struct{} {
	ids := make(map[ItemID]struct{})
	var collect func([]Item)
	collect = func(items []Item) {
		for i, item := range items {
			if i%64 == 0 && ctx.Err() != nil {
				return
			}
			ids[item.ID] = struct{}{}
			collect(item.Children)
		}
	}
	collect(items)
	return ids
}
