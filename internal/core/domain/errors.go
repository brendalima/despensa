package domain

import "strings"

// Domain errors carry a stable Code; internal/humanize owns the wording.
// This is where the project parts ways with venus-admin, whose
// ValidationError holds its own Portuguese message: here the only
// interface is a conversation, so a message built in the core would mean
// the same refusal worded in two layers.

const (
	CodeNotFound       = "not_found"
	CodeAmbiguous      = "ambiguous_item"
	CodeUnknownPortion = "unknown_portion"
	CodeInvalid        = "invalid"
	CodeDuplicateName  = "duplicate_name"
)

type NotFoundError struct{ msg string }

func (e *NotFoundError) Error() string { return e.msg }
func (e *NotFoundError) Code() string  { return CodeNotFound }

func NewNotFoundError(msg string) *NotFoundError { return &NotFoundError{msg: msg} }

// AmbiguousItemError reports that what the person said matches more than
// one item. Guessing would log rice against the arborio and nobody would
// notice for weeks.
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

// UnknownPortionError reports a word the item does not know that came
// with no estimate. The border asks the model to estimate rather than
// asking the person to measure.
type UnknownPortionError struct {
	Item    string
	Portion string
	Known   []string
}

func (e *UnknownPortionError) Error() string {
	return "unknown portion " + e.Portion + " for item " + e.Item
}
func (e *UnknownPortionError) Code() string { return CodeUnknownPortion }

func NewUnknownPortionError(item, portion string, known []string) *UnknownPortionError {
	return &UnknownPortionError{Item: item, Portion: portion, Known: known}
}

type ValidationError struct {
	Field string
	msg   string
}

func (e *ValidationError) Error() string { return "invalid " + e.Field + ": " + e.msg }
func (e *ValidationError) Code() string  { return CodeInvalid }

func NewValidationError(field, msg string) *ValidationError {
	return &ValidationError{Field: field, msg: msg}
}

type ConflictError struct{ Name string }

func (e *ConflictError) Error() string { return "item name already in use: " + e.Name }
func (e *ConflictError) Code() string  { return CodeDuplicateName }

func NewConflictError(name string) *ConflictError { return &ConflictError{Name: name} }
