package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/config"
)

// These are end-to-end tests: they start the whole service against a real
// PostgreSQL and a fake rate provider and talk to it over HTTP, the way a
// client would.

const (
	testDSNEnv = "TEST_DATABASE_URL"
	ciEnv      = "CI"

	// How long a test waits for the background worker to finish an update.
	waitFor = 10 * time.Second
	tick    = 20 * time.Millisecond
)

// testDatabaseURL returns the connection string of a fresh schema in the
// test database. The schema is dropped when the test ends. The test is
// skipped if TEST_DATABASE_URL is not set, except on CI, where it fails.
func testDatabaseURL(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		require.Empty(t, os.Getenv(ciEnv), "%s must be set on CI: end-to-end tests may not be skipped", testDSNEnv)
		t.Skipf("%s is not set, skipping end-to-end test", testDSNEnv)
	}

	schema := "e2e_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()

	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)

	_, err = admin.Exec(t.Context(), "CREATE SCHEMA "+quoted)
	require.NoError(t, err)

	t.Cleanup(func() {
		// t.Context is already cancelled when cleanups run.
		ctx := context.Background()

		_, dropErr := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		require.NoError(t, dropErr)
		require.NoError(t, admin.Close(ctx))
	})

	parsed, err := url.Parse(dsn)
	require.NoError(t, err)

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

// startService runs the service against a fresh schema and the given rate
// provider and returns its base URL. The service is shut down gracefully
// when the test ends, and the shutdown must be clean.
func startService(t *testing.T, providerURL string, overrides map[string]string) string {
	t.Helper()

	env := map[string]string{
		config.EnvDatabaseURL:    testDatabaseURL(t),
		config.EnvRatesAPIURL:    providerURL,
		config.EnvHTTPAddr:       "127.0.0.1:0",
		config.EnvWorkerInterval: "20ms",
	}
	for key, value := range overrides {
		env[key] = value
	}

	cfg, err := config.Load(func(key string) string { return env[key] })
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	application, err := newApp(ctx, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		cancel()
	}

	require.NoError(t, err)

	done := make(chan error, 1)

	go func() {
		done <- application.serve(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done, "the service must shut down cleanly")
	})

	return "http://" + application.addr()
}

// rateProvider starts a fake rate provider that answers with the given
// status and body and counts the requests it receives.
func rateProvider(t *testing.T, status int, body string) (providerURL string, requests *atomic.Int64) {
	t.Helper()

	requests = new(atomic.Int64)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server.URL, requests
}

// call sends a request to the service and returns the status code and the
// decoded JSON body.
func call(ctx context.Context, method, target, body string) (int, map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, nil, err
	}

	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, nil, err
	}

	return response.StatusCode, payload, nil
}

// mustCall is call for requests that are expected to go through.
func mustCall(t *testing.T, method, target, body string) (int, map[string]any) {
	t.Helper()

	status, payload, err := call(t.Context(), method, target, body)
	require.NoError(t, err)

	return status, payload
}

// requestUpdate asks the service to update EUR/MXN and returns the update id.
func requestUpdate(t *testing.T, baseURL string) string {
	t.Helper()

	status, payload := mustCall(t, http.MethodPost, baseURL+"/api/v1/quotes/updates", `{"base":"EUR","quote":"MXN"}`)
	require.Equal(t, http.StatusAccepted, status)

	id, ok := payload["update_id"].(string)
	require.True(t, ok, "update_id must be a string, got %v", payload["update_id"])

	return id
}

// waitForStatus polls the update until it reaches the given status and
// returns its final representation.
func waitForStatus(t *testing.T, baseURL, id, want string) map[string]any {
	t.Helper()

	var last map[string]any

	require.Eventually(t, func() bool {
		status, payload, err := call(t.Context(), http.MethodGet, baseURL+"/api/v1/quotes/updates/"+id, "")
		if err != nil || status != http.StatusOK {
			return false
		}

		last = payload

		return payload["status"] == want
	}, waitFor, tick, "update %s did not become %s", id, want)

	return last
}

func TestService_UpdateIsCarriedOutInTheBackground(t *testing.T) {
	t.Parallel()

	providerURL, providerRequests := rateProvider(t, http.StatusOK,
		`{"amount":1.0,"base":"EUR","date":"2026-10-02","rates":{"MXN":20.5806}}`)
	baseURL := startService(t, providerURL, nil)
	latestURL := baseURL + "/api/v1/quotes/latest?base=EUR&quote=MXN"

	// Nothing has been updated yet.
	status, payload := mustCall(t, http.MethodGet, latestURL, "")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, map[string]any{"code": "quote_not_found", "message": "the pair has no quote yet"}, payload["error"])

	id := requestUpdate(t, baseURL)

	completed := waitForStatus(t, baseURL, id, "completed")
	assert.Equal(t, id, completed["update_id"])
	assert.Equal(t, "EUR", completed["base"])
	assert.Equal(t, "MXN", completed["quote"])
	assert.Equal(t, "20.5806", completed["price"])
	assert.NotEmpty(t, completed["updated_at"])

	status, payload = mustCall(t, http.MethodGet, latestURL, "")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "20.5806", payload["price"])
	assert.Equal(t, completed["updated_at"], payload["updated_at"])

	assert.Equal(t, int64(1), providerRequests.Load(), "the provider must be asked exactly once")
}

func TestService_UpdateFailsAfterTheLastAttempt(t *testing.T) {
	t.Parallel()

	providerURL, providerRequests := rateProvider(t, http.StatusInternalServerError, `provider is down`)
	baseURL := startService(t, providerURL, map[string]string{
		// Short timeouts, so that the retry does not take half a minute.
		config.EnvWorkerMaxAttempts: "2",
		config.EnvRatesAPITimeout:   "100ms",
		config.EnvWorkerTimeout:     "200ms",
		config.EnvWorkerStaleAfter:  "300ms",
		config.EnvShutdownTimeout:   "5s",
	})

	id := requestUpdate(t, baseURL)

	failed := waitForStatus(t, baseURL, id, "failed")
	assert.Equal(t, "the quote could not be obtained from the rate provider", failed["error"])
	assert.NotContains(t, failed, "price")
	assert.Equal(t, int64(2), providerRequests.Load(), "the provider must be asked once per attempt")

	status, _ := mustCall(t, http.MethodGet, baseURL+"/api/v1/quotes/latest?base=EUR&quote=MXN", "")
	assert.Equal(t, http.StatusNotFound, status, "a failed update must not produce a quote")
}

func TestService_RepeatedRequestReturnsThePendingUpdate(t *testing.T) {
	t.Parallel()

	// A provider that does not answer until released keeps the update pending.
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release

		_, _ = w.Write([]byte(`{"base":"EUR","rates":{"MXN":20.5806}}`))
	}))
	t.Cleanup(provider.Close)

	baseURL := startService(t, provider.URL, nil)
	// Registered after startService, so it runs before the service shuts
	// down and lets the running batch finish.
	t.Cleanup(func() { close(release) })

	first := requestUpdate(t, baseURL)
	second := requestUpdate(t, baseURL)
	assert.Equal(t, first, second, "a repeated request must return the pending update")

	status, payload := mustCall(t, http.MethodGet, baseURL+"/api/v1/quotes/updates/"+first, "")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "pending", payload["status"])
}

func TestService_RejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	providerURL, _ := rateProvider(t, http.StatusOK, `{"base":"EUR","rates":{"MXN":20.5806}}`)
	baseURL := startService(t, providerURL, nil)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "currency is not supported",
			method:     http.MethodPost,
			path:       "/api/v1/quotes/updates",
			body:       `{"base":"EUR","quote":"JPY"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_currency",
		},
		{
			name:       "currency does not exist",
			method:     http.MethodPost,
			path:       "/api/v1/quotes/updates",
			body:       `{"base":"EUR","quote":"XYZ"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_currency",
		},
		{
			name:       "same base and quote currency",
			method:     http.MethodGet,
			path:       "/api/v1/quotes/latest?base=EUR&quote=EUR",
			body:       "",
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_pair",
		},
		{
			name:       "update does not exist",
			method:     http.MethodGet,
			path:       "/api/v1/quotes/updates/0199b0c2-7c3e-7a10-9f6b-3b1d2c4e5f60",
			body:       "",
			wantStatus: http.StatusNotFound,
			wantCode:   "update_not_found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, payload := mustCall(t, tt.method, baseURL+tt.path, tt.body)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantCode, payload["error"].(map[string]any)["code"])
		})
	}
}

func TestRun_FailsToStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr error
	}{
		{
			name:    "configuration is incomplete",
			env:     map[string]string{},
			wantErr: config.ErrMissingValue,
		},
		{
			name: "configuration is inconsistent",
			env: map[string]string{
				config.EnvDatabaseURL:      "postgres://127.0.0.1:1/quotes",
				config.EnvWorkerStaleAfter: "5s",
			},
			wantErr: config.ErrInconsistent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := run(t.Context(), func(key string) string { return tt.env[key] }, io.Discard)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRun_FailsToStartWithoutDatabase(t *testing.T) {
	t.Parallel()

	// Nothing listens on port 1.
	env := map[string]string{config.EnvDatabaseURL: "postgres://127.0.0.1:1/quotes?connect_timeout=2"}

	err := run(t.Context(), func(key string) string { return env[key] }, io.Discard)
	require.Error(t, err)
	assert.ErrorContains(t, err, "connect to database")
}
