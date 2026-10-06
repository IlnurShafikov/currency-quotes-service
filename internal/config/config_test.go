package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/config"
)

const databaseURL = "postgres://db:5432/quotes?sslmode=disable"

// defaults is the configuration produced when only the required variable is set.
func defaults() config.Config {
	return config.Config{
		HTTPAddr:          ":8080",
		ShutdownTimeout:   25 * time.Second,
		LogLevel:          slog.LevelInfo,
		DatabaseURL:       databaseURL,
		RatesAPIURL:       "https://api.frankfurter.dev/v1",
		RatesAPITimeout:   10 * time.Second,
		WorkerInterval:    time.Second,
		WorkerBatchSize:   10,
		WorkerMaxAttempts: 3,
		WorkerTimeout:     20 * time.Second,
		WorkerStaleAfter:  30 * time.Second,
		SchedulerInterval: 0,
	}
}

func defaultsWithLogLevel(level slog.Level) config.Config {
	cfg := defaults()
	cfg.LogLevel = level

	return cfg
}

// with returns the environment that has the required variable and the given
// overrides set.
func with(overrides map[string]string) map[string]string {
	env := map[string]string{config.EnvDatabaseURL: databaseURL}
	for key, value := range overrides {
		env[key] = value
	}

	return env
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		want    config.Config
		wantErr error
	}{
		{
			name:    "only the required variable is set: defaults are used",
			env:     with(nil),
			want:    defaults(),
			wantErr: nil,
		},
		{
			name: "every variable is set",
			env: map[string]string{
				config.EnvHTTPAddr:          "127.0.0.1:9090",
				config.EnvShutdownTimeout:   "1m",
				config.EnvLogLevel:          "debug",
				config.EnvDatabaseURL:       databaseURL,
				config.EnvRatesAPIURL:       "http://rates.internal/api",
				config.EnvRatesAPITimeout:   "3s",
				config.EnvWorkerInterval:    "500ms",
				config.EnvWorkerBatchSize:   "25",
				config.EnvWorkerMaxAttempts: "5",
				config.EnvWorkerTimeout:     "15s",
				config.EnvWorkerStaleAfter:  "45s",
				config.EnvSchedulerInterval: "30m",
			},
			want: config.Config{
				HTTPAddr:          "127.0.0.1:9090",
				ShutdownTimeout:   time.Minute,
				LogLevel:          slog.LevelDebug,
				DatabaseURL:       databaseURL,
				RatesAPIURL:       "http://rates.internal/api",
				RatesAPITimeout:   3 * time.Second,
				WorkerInterval:    500 * time.Millisecond,
				WorkerBatchSize:   25,
				WorkerMaxAttempts: 5,
				WorkerTimeout:     15 * time.Second,
				WorkerStaleAfter:  45 * time.Second,
				SchedulerInterval: 30 * time.Minute,
			},
			wantErr: nil,
		},
		{
			name:    "log level is case-insensitive",
			env:     with(map[string]string{config.EnvLogLevel: "WARN"}),
			want:    defaultsWithLogLevel(slog.LevelWarn),
			wantErr: nil,
		},
		{
			name:    "database url is missing",
			env:     map[string]string{},
			want:    config.Config{},
			wantErr: config.ErrMissingValue,
		},
		{
			name:    "duration is not parsable",
			env:     with(map[string]string{config.EnvWorkerInterval: "soon"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "duration has no unit",
			env:     with(map[string]string{config.EnvRatesAPITimeout: "10"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "duration is zero",
			env:     with(map[string]string{config.EnvWorkerInterval: "0s"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "duration is negative",
			env:     with(map[string]string{config.EnvWorkerInterval: "-1s"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "count is not an integer",
			env:     with(map[string]string{config.EnvWorkerBatchSize: "ten"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "count is zero",
			env:     with(map[string]string{config.EnvWorkerMaxAttempts: "0"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "count is negative",
			env:     with(map[string]string{config.EnvWorkerBatchSize: "-5"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "scheduler interval is not parsable",
			env:     with(map[string]string{config.EnvSchedulerInterval: "hourly"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "scheduler interval is zero: leave the variable unset to turn the scheduler off",
			env:     with(map[string]string{config.EnvSchedulerInterval: "0s"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "log level is unknown",
			env:     with(map[string]string{config.EnvLogLevel: "verbose"}),
			want:    config.Config{},
			wantErr: config.ErrInvalidValue,
		},
		{
			name:    "provider timeout does not fit into the worker timeout",
			env:     with(map[string]string{config.EnvRatesAPITimeout: "20s"}),
			want:    config.Config{},
			wantErr: config.ErrInconsistent,
		},
		{
			name:    "worker timeout does not end before the claim goes stale",
			env:     with(map[string]string{config.EnvWorkerStaleAfter: "20s"}),
			want:    config.Config{},
			wantErr: config.ErrInconsistent,
		},
		{
			name:    "shutdown does not leave a running batch time to finish",
			env:     with(map[string]string{config.EnvShutdownTimeout: "20s"}),
			want:    config.Config{},
			wantErr: config.ErrInconsistent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(func(key string) string { return tt.env[key] })
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestLoad_ReportsEveryProblemAtOnce(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		config.EnvWorkerInterval:  "soon",
		config.EnvWorkerBatchSize: "ten",
		config.EnvLogLevel:        "verbose",
	}

	_, err := config.Load(func(key string) string { return env[key] })

	require.ErrorIs(t, err, config.ErrMissingValue)
	require.ErrorIs(t, err, config.ErrInvalidValue)
	require.ErrorContains(t, err, config.EnvDatabaseURL)
	require.ErrorContains(t, err, config.EnvWorkerInterval)
	require.ErrorContains(t, err, config.EnvWorkerBatchSize)
	require.ErrorContains(t, err, config.EnvLogLevel)
}
