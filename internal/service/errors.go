package service

import "errors"

var (
	// ErrClaimLost is returned when the outcome of an update request cannot
	// be stored because another worker has claimed the request in the
	// meantime or it has already been finished. The result must be discarded.
	ErrClaimLost = errors.New("update request claim lost")
	// ErrAttemptsExhausted is the reason an update request is failed with
	// when all its attempts ended without a result, e.g. the worker crashed
	// every time.
	ErrAttemptsExhausted = errors.New("attempts exhausted")
	// ErrInvalidProcessorConfig is returned when a [ProcessorConfig] is unusable.
	ErrInvalidProcessorConfig = errors.New("invalid processor config")
)
