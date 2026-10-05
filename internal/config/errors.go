package config

import "errors"

var (
	// ErrMissingValue is returned when a required variable is not set.
	ErrMissingValue = errors.New("required variable is not set")
	// ErrInvalidValue is returned when a variable cannot be parsed or its
	// value is out of range.
	ErrInvalidValue = errors.New("invalid value")
	// ErrInconsistent is returned when values are valid on their own but
	// contradict each other.
	ErrInconsistent = errors.New("inconsistent configuration")
)
