package domain

import (
	"fmt"

	"github.com/google/uuid"
)

// UpdateID identifies an [UpdateRequest].
type UpdateID uuid.UUID

// QuoteID identifies a [Quote].
type QuoteID uuid.UUID

// NewUpdateID returns a new time-ordered (UUIDv7) update request identifier.
func NewUpdateID() (UpdateID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return UpdateID{}, fmt.Errorf("generate update id: %w", err)
	}

	return UpdateID(id), nil
}

// NewQuoteID returns a new time-ordered (UUIDv7) quote identifier.
func NewQuoteID() (QuoteID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return QuoteID{}, fmt.Errorf("generate quote id: %w", err)
	}

	return QuoteID(id), nil
}

// ParseUpdateID parses the textual form of an update request identifier.
// It returns [ErrInvalidID] if s is not a valid UUID.
func ParseUpdateID(s string) (UpdateID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return UpdateID{}, fmt.Errorf("%w: %q", ErrInvalidID, s)
	}

	return UpdateID(id), nil
}

// String returns the canonical textual form of the identifier.
func (id UpdateID) String() string { return uuid.UUID(id).String() }

// String returns the canonical textual form of the identifier.
func (id QuoteID) String() string { return uuid.UUID(id).String() }
