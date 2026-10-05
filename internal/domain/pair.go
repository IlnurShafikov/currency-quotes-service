package domain

import "fmt"

// Pair is a currency pair: the price of one unit of Base expressed in Quote.
type Pair struct {
	Base  Currency
	Quote Currency
}

// NewPair returns a pair of two distinct currencies.
// It returns [ErrInvalidPair] if base and quote are the same.
func NewPair(base, quote Currency) (Pair, error) {
	if base == quote {
		return Pair{}, fmt.Errorf("%w: %s", ErrInvalidPair, base)
	}

	return Pair{Base: base, Quote: quote}, nil
}

// String returns the pair in the conventional "BASE/QUOTE" form.
func (p Pair) String() string {
	return string(p.Base) + "/" + string(p.Quote)
}
