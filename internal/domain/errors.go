// Package domain contains the business model of the quotes service:
// currencies, currency pairs, quotes and quote update requests.
//
// The package has no knowledge of HTTP, the database or the rate provider.
// Everything else in the service depends on it, and it depends on nothing
// but the standard library, a decimal type and a UUID type.
package domain

import "errors"

var (
	// ErrInvalidCurrency is returned when a currency code is malformed.
	ErrInvalidCurrency = errors.New("invalid currency code")
	// ErrInvalidPair is returned when base and quote currencies are the same.
	ErrInvalidPair = errors.New("base and quote currencies must differ")
	// ErrInvalidPrice is returned when a quote price is zero or negative.
	ErrInvalidPrice = errors.New("price must be positive")
	// ErrInvalidID is returned when an identifier is not a valid UUID.
	ErrInvalidID = errors.New("invalid identifier")
	// ErrUpdateAlreadyFinished is returned on an attempt to complete or fail
	// an update request that is no longer pending.
	ErrUpdateAlreadyFinished = errors.New("update request is already finished")
	// ErrEmptyFailureReason is returned when an update request is failed
	// without a reason.
	ErrEmptyFailureReason = errors.New("failure reason must not be empty")
)
