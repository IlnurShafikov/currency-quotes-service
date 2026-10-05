// Package system provides the real implementations of the Clock and
// IDGenerator ports: the ones that read the wall clock and generate random
// identifiers. Tests replace them with deterministic stand-ins.
package system

import (
	"fmt"
	"time"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

var (
	_ service.Clock       = Clock{}
	_ service.IDGenerator = IDs{}
)

// Clock reads the wall clock.
type Clock struct{}

// Now returns the current time in UTC, truncated to microseconds.
//
// PostgreSQL stores timestamps with microsecond precision. Truncating here
// keeps a value identical before it is stored and after it is read back.
func (Clock) Now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// IDs generates time-ordered (UUIDv7) identifiers.
type IDs struct{}

// NewUpdateID returns a new update request identifier.
func (IDs) NewUpdateID() (domain.UpdateID, error) {
	id, err := domain.NewUpdateID()
	if err != nil {
		return domain.UpdateID{}, fmt.Errorf("system ids: %w", err)
	}

	return id, nil
}

// NewQuoteID returns a new quote identifier.
func (IDs) NewQuoteID() (domain.QuoteID, error) {
	id, err := domain.NewQuoteID()
	if err != nil {
		return domain.QuoteID{}, fmt.Errorf("system ids: %w", err)
	}

	return id, nil
}
