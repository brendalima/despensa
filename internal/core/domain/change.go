package domain

// ItemChange is an edit to an item. Every field is a pointer or a slice
// so that "not mentioned" and "set to empty" stay different things: the
// model asked to change the threshold must not wipe the category it never
// mentioned.
type ItemChange struct {
	ItemID string

	Name      *string
	Category  *string
	Threshold *Amount

	// Portions are added or corrected, not replaced: a size given here
	// is the person's own, so it outranks any earlier estimate.
	Portions []Portion

	// Aliases, when not nil, replaces the whole list.
	Aliases []string

	// Archive, when set, archives or brings the item back.
	Archive *bool
}

// Empty reports that the change asks for nothing, which is a mistake
// worth refusing rather than a no-op to hide.
func (c ItemChange) Empty() bool {
	return c.Name == nil && c.Category == nil && c.Threshold == nil &&
		len(c.Portions) == 0 && c.Aliases == nil && c.Archive == nil
}
