package domain

import (
	"fmt"

	"golang.org/x/text/currency"
)

// Currency is an ISO 4217 currency code in canonical upper-case form, e.g. "EUR".
type Currency string

// NewCurrency validates code against ISO 4217 and returns it as a [Currency]
// in upper case. It returns [ErrInvalidCurrency] if code is not a known
// ISO 4217 currency code.
//
// Non-ISO tickers such as crypto assets ("USDT", "BTC") are rejected. The
// storage schema already allows codes of up to 10 characters, so supporting
// them only requires relaxing this function.
func NewCurrency(code string) (Currency, error) {
	unit, err := currency.ParseISO(code)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidCurrency, code)
	}

	return Currency(unit.String()), nil
}

// String returns the currency code.
func (c Currency) String() string { return string(c) }
