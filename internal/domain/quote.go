package domain

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Quote is an immutable fact: the price of a currency pair at a point in time.
type Quote struct {
	ID         QuoteID
	Pair       Pair
	Price      decimal.Decimal
	ObtainedAt time.Time
}

// NewQuote returns a quote for pair obtained at obtainedAt.
// It returns [ErrInvalidPrice] if price is not positive.
func NewQuote(id QuoteID, pair Pair, price decimal.Decimal, obtainedAt time.Time) (Quote, error) {
	if !price.IsPositive() {
		return Quote{}, fmt.Errorf("%w: %s", ErrInvalidPrice, price)
	}

	return Quote{
		ID:         id,
		Pair:       pair,
		Price:      price,
		ObtainedAt: obtainedAt.UTC(),
	}, nil
}
