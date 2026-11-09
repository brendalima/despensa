package domain

// ItemChange is an edit to an item. Fields are pointers so that "not
// mentioned" and "set to empty" stay different: changing the threshold
// must not wipe the category.
type ItemChange struct {
	ItemID string

	Name      *string
	Category  *string
	Threshold *Amount

	// Shopping overrules the minimum, in either direction.
	Shopping *ShoppingMode

	// Portions are added or corrected, not replaced: a size given here
	// is the person's own, so it outranks any estimate.
	Portions []Portion

	// Aliases, when not nil, replaces the whole list.
	Aliases []string

	Archive *bool
}

// Empty reports that the change asks for nothing: worth refusing rather
// than hiding as a no-op.
func (c ItemChange) Empty() bool {
	return c.Name == nil && c.Category == nil && c.Threshold == nil &&
		c.Shopping == nil && len(c.Portions) == 0 && c.Aliases == nil &&
		c.Archive == nil
}
