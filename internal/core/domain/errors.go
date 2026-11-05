package domain

import "strings"

// Domain errors. The repository translates infrastructure errors into
// these, and the border (internal/humanize) turns these into Portuguese,
// so the core never holds a sentence meant for a person.
//
// This is the one place this project parts ways with venus-admin, where
// ValidationError carries its own Portuguese Message. Here the only user
// interface is a conversation, and a message built in the core would mean
// the same refusal was worded in two layers. So each error carries a
// stable Code instead, and the border owns the wording.

// Error codes, stable because the border switches on them.
const (
	CodeNotFound       = "not_found"
	CodeAmbiguous      = "ambiguous_item"
	CodeUnknownPortion = "unknown_portion"
	CodeInvalid        = "invalid"
	CodeAlreadyUndone  = "already_undone"
	CodeDuplicateName  = "duplicate_name"
)

type NotFoundError struct{ msg string }

func (e *NotFoundError) Error() string { return e.msg }
func (e *NotFoundError) Code() string  { return CodeNotFound }

func NewNotFoundError(msg string) *NotFoundError { return &NotFoundError{msg: msg} }

// AmbiguousItemError reports that what the person said matches more than
// one item. Candidates are the names, so the border can ask which one —
// guessing would silently log a sale of rice against the arborio.
type AmbiguousItemError struct {
	Said       string
	Candidates []string
}

func (e *AmbiguousItemError) Error() string {
	return "ambiguous item " + e.Said + ": " + strings.Join(e.Candidates, ", ")
}
func (e *AmbiguousItemError) Code() string { return CodeAmbiguous }

func NewAmbiguousItemError(said string, candidates []string) *AmbiguousItemError {
	return &AmbiguousItemError{Said: said, Candidates: candidates}
}

// UnknownPortionError reports a word the item does not know and that came
// with no estimate. The border asks the model to estimate rather than
// asking the person to measure.
type UnknownPortionError struct {
	Item    string
	Portion string
	// Known is the item's vocabulary, so the answer can offer it.
	Known []string
}

func (e *UnknownPortionError) Error() string {
	return "unknown portion " + e.Portion + " for item " + e.Item
}
func (e *UnknownPortionError) Code() string { return CodeUnknownPortion }

func NewUnknownPortionError(item, portion string, known []string) *UnknownPortionError {
	return &UnknownPortionError{Item: item, Portion: portion, Known: known}
}

// ValidationError carries the field that failed and a stable code. Error()
// is in English, lowercase and without a full stop, like every other
// error in the project.
type ValidationError struct {
	Field string
	code  string
	msg   string
}

func (e *ValidationError) Error() string { return "invalid " + e.Field + ": " + e.msg }
func (e *ValidationError) Code() string {
	if e.code == "" {
		return CodeInvalid
	}
	return e.code
}

func NewValidationError(field, msg string) *ValidationError {
	return &ValidationError{Field: field, msg: msg}
}

func NewCodedValidationError(field, code, msg string) *ValidationError {
	return &ValidationError{Field: field, code: code, msg: msg}
}

// ConflictError reports a name already in use by another item.
type ConflictError struct{ Name string }

func (e *ConflictError) Error() string { return "item name already in use: " + e.Name }
func (e *ConflictError) Code() string  { return CodeDuplicateName }

func NewConflictError(name string) *ConflictError { return &ConflictError{Name: name} }
