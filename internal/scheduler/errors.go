package scheduler

import "errors"

// ErrInvalidInterval is returned when the refresh interval is not positive.
var ErrInvalidInterval = errors.New("scheduler interval must be positive")
