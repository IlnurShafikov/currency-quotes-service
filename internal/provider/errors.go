package provider

import "errors"

var (
	// ErrInvalidBaseURL is returned when the provider base URL is unusable.
	ErrInvalidBaseURL = errors.New("invalid provider base url")
	// ErrUnexpectedStatus is returned when the provider answers with a
	// status other than 200 OK.
	ErrUnexpectedStatus = errors.New("unexpected provider response status")
	// ErrMalformedResponse is returned when the provider response cannot be
	// understood.
	ErrMalformedResponse = errors.New("malformed provider response")
	// ErrRateMissing is returned when the provider response does not contain
	// a rate for the requested currency.
	ErrRateMissing = errors.New("rate is missing in provider response")
)
