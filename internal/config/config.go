// Package config loads the service configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// Names of the environment variables the service reads.
const (
	EnvHTTPAddr          = "HTTP_ADDR"
	EnvShutdownTimeout   = "SHUTDOWN_TIMEOUT"
	EnvLogLevel          = "LOG_LEVEL"
	EnvDatabaseURL       = "DATABASE_URL"
	EnvRatesAPIURL       = "RATES_API_URL"
	EnvRatesAPITimeout   = "RATES_API_TIMEOUT"
	EnvWorkerInterval    = "WORKER_INTERVAL"
	EnvWorkerBatchSize   = "WORKER_BATCH_SIZE"
	EnvWorkerMaxAttempts = "WORKER_MAX_ATTEMPTS"
	EnvWorkerTimeout     = "WORKER_TIMEOUT"
	EnvWorkerStaleAfter  = "WORKER_STALE_AFTER"
	EnvSchedulerInterval = "SCHEDULER_INTERVAL"
)

// Values used when a variable is not set.
const (
	DefaultHTTPAddr          = ":8080"
	DefaultShutdownTimeout   = 25 * time.Second
	DefaultLogLevel          = slog.LevelInfo
	DefaultRatesAPIURL       = "https://api.frankfurter.dev/v1"
	DefaultRatesAPITimeout   = 10 * time.Second
	DefaultWorkerInterval    = time.Second
	DefaultWorkerBatchSize   = 10
	DefaultWorkerMaxAttempts = 3
	DefaultWorkerTimeout     = 20 * time.Second
	DefaultWorkerStaleAfter  = 30 * time.Second

	// DefaultSchedulerInterval is zero: the scheduler is off unless an
	// interval is configured.
	DefaultSchedulerInterval time.Duration = 0
)

// Config is the configuration of the service.
type Config struct {
	// HTTPAddr is the address the HTTP server listens on.
	HTTPAddr string
	// ShutdownTimeout is how long the service waits for in-flight work to
	// finish after it is asked to stop.
	ShutdownTimeout time.Duration
	// LogLevel is the minimum level of log records that are written.
	LogLevel slog.Level

	// DatabaseURL is the PostgreSQL connection string. Required.
	DatabaseURL string

	// RatesAPIURL is the base URL of the exchange rate provider.
	RatesAPIURL string
	// RatesAPITimeout bounds one request to the provider.
	RatesAPITimeout time.Duration

	// WorkerInterval is how often the worker looks for pending updates.
	WorkerInterval time.Duration
	// WorkerBatchSize is how many updates the worker processes at once.
	WorkerBatchSize int
	// WorkerMaxAttempts is how many times an update is tried before it fails.
	WorkerMaxAttempts int
	// WorkerTimeout bounds the processing of one batch.
	WorkerTimeout time.Duration
	// WorkerStaleAfter is how long a claimed update is left to its worker
	// before another worker may take it over.
	WorkerStaleAfter time.Duration

	// SchedulerInterval is how often stale quotes are refreshed without a
	// client asking, and at the same time how old a quote may get before it
	// counts as stale. Zero turns the scheduler off: quotes are then updated
	// only on request.
	SchedulerInterval time.Duration
}

// Load reads the configuration using getenv, which is normally [os.Getenv].
// Variables that are not set fall back to their defaults.
//
// It reports every problem it finds at once, so that a broken configuration
// can be fixed in one go. The returned error wraps [ErrMissingValue],
// [ErrInvalidValue] or [ErrInconsistent].
func Load(getenv func(string) string) (Config, error) {
	env := reader{getenv: getenv, errs: nil}

	cfg := Config{
		HTTPAddr:          env.text(EnvHTTPAddr, DefaultHTTPAddr),
		ShutdownTimeout:   env.duration(EnvShutdownTimeout, DefaultShutdownTimeout),
		LogLevel:          env.logLevel(EnvLogLevel, DefaultLogLevel),
		DatabaseURL:       env.required(EnvDatabaseURL),
		RatesAPIURL:       env.text(EnvRatesAPIURL, DefaultRatesAPIURL),
		RatesAPITimeout:   env.duration(EnvRatesAPITimeout, DefaultRatesAPITimeout),
		WorkerInterval:    env.duration(EnvWorkerInterval, DefaultWorkerInterval),
		WorkerBatchSize:   env.count(EnvWorkerBatchSize, DefaultWorkerBatchSize),
		WorkerMaxAttempts: env.count(EnvWorkerMaxAttempts, DefaultWorkerMaxAttempts),
		WorkerTimeout:     env.duration(EnvWorkerTimeout, DefaultWorkerTimeout),
		WorkerStaleAfter:  env.duration(EnvWorkerStaleAfter, DefaultWorkerStaleAfter),
		SchedulerInterval: env.duration(EnvSchedulerInterval, DefaultSchedulerInterval),
	}

	if err := errors.Join(env.errs...); err != nil {
		return Config{}, err
	}

	if err := cfg.checkConsistency(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// checkConsistency verifies the rules that tie the timeouts together:
//
//	RATES_API_TIMEOUT < WORKER_TIMEOUT < WORKER_STALE_AFTER
//	                    WORKER_TIMEOUT < SHUTDOWN_TIMEOUT
//
// A provider call must fit into a batch, a batch must end before its claim
// goes stale, and a shutdown must leave a running batch time to finish.
func (c Config) checkConsistency() error {
	var errs []error

	if c.RatesAPITimeout >= c.WorkerTimeout {
		errs = append(errs, fmt.Errorf("%w: %s (%s) must be less than %s (%s)",
			ErrInconsistent, EnvRatesAPITimeout, c.RatesAPITimeout, EnvWorkerTimeout, c.WorkerTimeout))
	}

	if c.WorkerTimeout >= c.WorkerStaleAfter {
		errs = append(errs, fmt.Errorf("%w: %s (%s) must be less than %s (%s)",
			ErrInconsistent, EnvWorkerTimeout, c.WorkerTimeout, EnvWorkerStaleAfter, c.WorkerStaleAfter))
	}

	if c.WorkerTimeout >= c.ShutdownTimeout {
		errs = append(errs, fmt.Errorf("%w: %s (%s) must be less than %s (%s)",
			ErrInconsistent, EnvWorkerTimeout, c.WorkerTimeout, EnvShutdownTimeout, c.ShutdownTimeout))
	}

	return errors.Join(errs...)
}

// reader reads typed values from the environment and collects the problems
// it runs into instead of stopping at the first one.
type reader struct {
	getenv func(string) string
	errs   []error
}

// text returns the value of key, or fallback if it is not set.
func (r *reader) text(key, fallback string) string {
	if value := r.getenv(key); value != "" {
		return value
	}

	return fallback
}

// required returns the value of key and records a problem if it is not set.
func (r *reader) required(key string) string {
	value := r.getenv(key)
	if value == "" {
		r.errs = append(r.errs, fmt.Errorf("%s: %w", key, ErrMissingValue))
	}

	return value
}

// duration returns the value of key as a positive duration such as "10s" or
// "1m30s", or fallback if it is not set.
func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	raw := r.getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %w: %q is not a duration", key, ErrInvalidValue, raw))

		return 0
	}

	if value <= 0 {
		r.errs = append(r.errs, fmt.Errorf("%s: %w: %s is not positive", key, ErrInvalidValue, value))

		return 0
	}

	return value
}

// count returns the value of key as a positive integer, or fallback if it is
// not set.
func (r *reader) count(key string, fallback int) int {
	raw := r.getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %w: %q is not an integer", key, ErrInvalidValue, raw))

		return 0
	}

	if value <= 0 {
		r.errs = append(r.errs, fmt.Errorf("%s: %w: %d is not positive", key, ErrInvalidValue, value))

		return 0
	}

	return value
}

// logLevel returns the value of key as a log level: debug, info, warn or
// error, in any letter case. It returns fallback if the variable is not set.
func (r *reader) logLevel(key string, fallback slog.Level) slog.Level {
	raw := r.getenv(key)
	if raw == "" {
		return fallback
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %w: %q is not a log level", key, ErrInvalidValue, raw))

		return fallback
	}

	return level
}
