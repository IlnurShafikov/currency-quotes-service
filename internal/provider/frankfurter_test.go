package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/provider"
)

// liveEnv names the environment variable that enables the test against the
// real Frankfurter API.
const liveEnv = "FRANKFURTER_LIVE"

var eurMXN = domain.Pair{Base: "EUR", Quote: "MXN"}

// newServer starts a fake provider that answers every request with the given
// status and body. It is shut down when the test ends.
func newServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server
}

func newFrankfurter(t *testing.T, baseURL string) *provider.Frankfurter {
	t.Helper()

	frankfurter, err := provider.NewFrankfurter(baseURL, &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)

	return frankfurter
}

func TestNewFrankfurter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		wantOK  bool
		wantErr error
	}{
		{
			name:    "https url",
			baseURL: "https://api.frankfurter.dev/v1",
			wantOK:  true,
			wantErr: nil,
		},
		{
			name:    "http url",
			baseURL: "http://localhost:8080",
			wantOK:  true,
			wantErr: nil,
		},
		{
			name:    "empty",
			baseURL: "",
			wantOK:  false,
			wantErr: provider.ErrInvalidBaseURL,
		},
		{
			name:    "no scheme",
			baseURL: "api.frankfurter.dev/v1",
			wantOK:  false,
			wantErr: provider.ErrInvalidBaseURL,
		},
		{
			name:    "unsupported scheme",
			baseURL: "ftp://api.frankfurter.dev/v1",
			wantOK:  false,
			wantErr: provider.ErrInvalidBaseURL,
		},
		{
			name:    "no host",
			baseURL: "https:///v1",
			wantOK:  false,
			wantErr: provider.ErrInvalidBaseURL,
		},
		{
			name:    "unparsable",
			baseURL: "https://api.frankfurter.dev:port",
			wantOK:  false,
			wantErr: provider.ErrInvalidBaseURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := provider.NewFrankfurter(tt.baseURL, http.DefaultClient)
			assert.Equal(t, tt.wantOK, got != nil)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestFrankfurter_Rate(t *testing.T) {
	t.Parallel()

	noRate := decimal.Decimal{}

	tests := []struct {
		name    string
		status  int
		body    string
		want    decimal.Decimal
		wantErr error
	}{
		{
			name:    "rate is returned",
			status:  http.StatusOK,
			body:    `{"amount":1.0,"base":"EUR","date":"2026-10-02","rates":{"MXN":20.5806}}`,
			want:    decimal.RequireFromString("20.5806"),
			wantErr: nil,
		},
		{
			name:    "every digit of the rate is kept",
			status:  http.StatusOK,
			body:    `{"amount":1.0,"base":"EUR","date":"2026-10-02","rates":{"MXN":20.123456789012345678}}`,
			want:    decimal.RequireFromString("20.123456789012345678"),
			wantErr: nil,
		},
		{
			name:    "rate of the requested currency is picked among others",
			status:  http.StatusOK,
			body:    `{"amount":1.0,"base":"EUR","date":"2026-10-02","rates":{"USD":1.1225,"MXN":20.5806}}`,
			want:    decimal.RequireFromString("20.5806"),
			wantErr: nil,
		},
		{
			name:    "currency is unknown to the provider",
			status:  http.StatusNotFound,
			body:    `{"message":"not found"}`,
			want:    noRate,
			wantErr: provider.ErrUnexpectedStatus,
		},
		{
			name:    "provider limits the request rate",
			status:  http.StatusTooManyRequests,
			body:    `{"message":"too many requests"}`,
			want:    noRate,
			wantErr: provider.ErrUnexpectedStatus,
		},
		{
			name:    "provider is broken",
			status:  http.StatusInternalServerError,
			body:    `internal server error`,
			want:    noRate,
			wantErr: provider.ErrUnexpectedStatus,
		},
		{
			name:    "body is not json",
			status:  http.StatusOK,
			body:    `<html>maintenance</html>`,
			want:    noRate,
			wantErr: provider.ErrMalformedResponse,
		},
		{
			name:    "body is empty",
			status:  http.StatusOK,
			body:    ``,
			want:    noRate,
			wantErr: provider.ErrMalformedResponse,
		},
		{
			name:    "rate is not a number",
			status:  http.StatusOK,
			body:    `{"amount":1.0,"base":"EUR","date":"2026-10-02","rates":{"MXN":"n/a"}}`,
			want:    noRate,
			wantErr: provider.ErrMalformedResponse,
		},
		{
			name:    "rates are for another base currency",
			status:  http.StatusOK,
			body:    `{"amount":1.0,"base":"USD","date":"2026-10-02","rates":{"MXN":18.335}}`,
			want:    noRate,
			wantErr: provider.ErrMalformedResponse,
		},
		{
			name:    "requested currency is missing",
			status:  http.StatusOK,
			body:    `{"amount":1.0,"base":"EUR","date":"2026-10-02","rates":{"USD":1.1225}}`,
			want:    noRate,
			wantErr: provider.ErrRateMissing,
		},
		{
			name:    "no rates at all",
			status:  http.StatusOK,
			body:    `{"amount":1.0,"base":"EUR","date":"2026-10-02","rates":{}}`,
			want:    noRate,
			wantErr: provider.ErrRateMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := newServer(t, tt.status, tt.body)
			frankfurter := newFrankfurter(t, server.URL)

			got, err := frankfurter.Rate(t.Context(), eurMXN)
			assert.Equal(t, tt.want, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestFrankfurter_Rate_SendsExpectedRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		basePath string
		wantPath string
	}{
		{
			name:     "base url with a path",
			basePath: "/v1",
			wantPath: "/v1/latest",
		},
		{
			name:     "base url with a trailing slash",
			basePath: "/v1/",
			wantPath: "/v1/latest",
		},
		{
			name:     "base url without a path",
			basePath: "",
			wantPath: "/latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The handler runs on another goroutine; the channel hands the
			// request over to the test without a data race.
			requests := make(chan *http.Request, 1)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r

				_, _ = w.Write([]byte(`{"base":"EUR","rates":{"MXN":20.5806}}`))
			}))
			t.Cleanup(server.Close)

			_, err := newFrankfurter(t, server.URL+tt.basePath).Rate(t.Context(), eurMXN)
			require.NoError(t, err)

			got := <-requests
			assert.Equal(t, http.MethodGet, got.Method)
			assert.Equal(t, tt.wantPath, got.URL.Path)
			assert.Equal(t, "EUR", got.URL.Query().Get("base"))
			assert.Equal(t, "MXN", got.URL.Query().Get("symbols"))
			assert.Equal(t, "application/json", got.Header.Get("Accept"))
		})
	}
}

func TestFrankfurter_Rate_RequestFails(t *testing.T) {
	t.Parallel()

	cancelled := func(t *testing.T) context.Context {
		t.Helper()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		return ctx
	}

	expired := func(t *testing.T) context.Context {
		t.Helper()

		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		t.Cleanup(cancel)

		return ctx
	}

	tests := []struct {
		name    string
		ctx     func(t *testing.T) context.Context
		wantErr error
	}{
		{
			name:    "context is cancelled",
			ctx:     cancelled,
			wantErr: context.Canceled,
		},
		{
			name:    "context deadline has passed",
			ctx:     expired,
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := newServer(t, http.StatusOK, `{"base":"EUR","rates":{"MXN":20.5806}}`)

			got, err := newFrankfurter(t, server.URL).Rate(tt.ctx(t), eurMXN)
			assert.Equal(t, decimal.Decimal{}, got)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestFrankfurter_Rate_ProviderIsUnreachable(t *testing.T) {
	t.Parallel()

	server := newServer(t, http.StatusOK, `{}`)
	server.Close()

	got, err := newFrankfurter(t, server.URL).Rate(t.Context(), eurMXN)
	assert.Equal(t, decimal.Decimal{}, got)
	require.Error(t, err)
}

// TestFrankfurter_Rate_Live checks the adapter against the real API. It
// needs network access and depends on a third party, so it only runs when
// FRANKFURTER_LIVE is set.
func TestFrankfurter_Rate_Live(t *testing.T) {
	t.Parallel()

	if os.Getenv(liveEnv) == "" {
		t.Skipf("%s is not set, skipping the test against the real API", liveEnv)
	}

	got, err := newFrankfurter(t, "https://api.frankfurter.dev/v1").Rate(t.Context(), eurMXN)
	require.NoError(t, err)
	assert.True(t, got.IsPositive(), "EUR/MXN rate must be positive, got %s", got)
}
