package handler_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/handler"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

const (
	updateIDText = "0199b0c2-7c3e-7a10-9f6b-3b1d2c4e5f60"

	updatesURL = "/api/v1/quotes/updates"
	latestURL  = "/api/v1/quotes/latest"

	internalErrorBody = `{"error":{"code":"internal_error","message":"internal server error"}}`
	malformedBody     = `{"error":{"code":"malformed_request",` +
		`"message":"request body must be a JSON object with \"base\" and \"quote\" fields"}}`
)

var (
	updateID    = domain.UpdateID(uuid.MustParse(updateIDText))
	quoteID     = domain.QuoteID(uuid.MustParse("0199b0c2-8d4f-7b21-a07c-4c2e3d5f6a71"))
	eurMXN      = domain.Pair{Base: "EUR", Quote: "MXN"}
	requestedAt = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	obtainedAt  = time.Date(2026, 10, 4, 12, 0, 2, 0, time.UTC)

	// Errors as the service layer returns them.
	errInvalidBase  = fmt.Errorf("base currency: %w: %q", domain.ErrInvalidCurrency, "EURO")
	errInvalidQuote = fmt.Errorf("quote currency: %w: %q", domain.ErrInvalidCurrency, "")
	errSamePair     = fmt.Errorf("currency pair: %w: %s", domain.ErrInvalidPair, "EUR")
	errUnsupported  = fmt.Errorf("%w: %s", domain.ErrUnsupportedCurrency, "EUR/JPY")
	errNoUpdate     = fmt.Errorf("get update: %w", domain.ErrUpdateNotFound)
	errNoQuote      = fmt.Errorf("get latest quote for EUR/MXN: %w", domain.ErrQuoteNotFound)
	errDatabase     = errors.New("connect to 10.0.0.5:5432: password authentication failed")
)

// stubService is a handler.QuoteService that returns preset values and
// records the calls it received.
type stubService struct {
	updateID   domain.UpdateID
	requestErr error
	result     service.UpdateResult
	getErr     error
	latest     domain.Quote
	latestErr  error

	calls []string
}

func (s *stubService) RequestUpdate(_ context.Context, base, quote string) (domain.UpdateID, error) {
	s.calls = append(s.calls, fmt.Sprintf("RequestUpdate(%q, %q)", base, quote))

	return s.updateID, s.requestErr
}

func (s *stubService) GetUpdate(_ context.Context, id domain.UpdateID) (service.UpdateResult, error) {
	s.calls = append(s.calls, fmt.Sprintf("GetUpdate(%s)", id))

	return s.result, s.getErr
}

func (s *stubService) GetLatestQuote(_ context.Context, base, quote string) (domain.Quote, error) {
	s.calls = append(s.calls, fmt.Sprintf("GetLatestQuote(%q, %q)", base, quote))

	return s.latest, s.latestErr
}

// serve sends one request to the API backed by svc and returns the response
// together with everything the handler logged.
func serve(t *testing.T, svc *stubService, method, target, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()

	var logs bytes.Buffer

	api := handler.New(svc, slog.New(slog.NewTextHandler(&logs, nil))).Routes()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))

	api.ServeHTTP(recorder, request)

	return recorder, logs.String()
}

func pendingResult() service.UpdateResult {
	return service.UpdateResult{
		Request: domain.NewUpdateRequest(updateID, eurMXN, requestedAt),
		Quote:   nil,
	}
}

func completedResult() service.UpdateResult {
	result := pendingResult()
	result.Request.Status = domain.UpdateCompleted
	result.Request.QuoteID = new(quoteID)
	result.Request.Attempts = 1
	result.Request.FinishedAt = new(obtainedAt)
	result.Quote = new(eurMXNQuote())

	return result
}

func failedResult() service.UpdateResult {
	result := pendingResult()
	result.Request.Status = domain.UpdateFailed
	result.Request.Error = "fetch rate for EUR/MXN: frankfurter: Get \"https://api.frankfurter.dev/v1/latest\": timeout"
	result.Request.Attempts = 3
	result.Request.FinishedAt = new(obtainedAt)

	return result
}

func eurMXNQuote() domain.Quote {
	return domain.Quote{
		ID:         quoteID,
		Pair:       eurMXN,
		Price:      decimal.RequireFromString("20.5806"),
		ObtainedAt: obtainedAt,
	}
}

func TestHandler_RequestUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         string
		requestErr   error
		wantStatus   int
		wantBody     string
		wantLocation string
		wantCalls    []string
		wantLogged   bool
	}{
		{
			name:         "update is accepted",
			body:         `{"base":"EUR","quote":"MXN"}`,
			requestErr:   nil,
			wantStatus:   http.StatusAccepted,
			wantBody:     `{"update_id":"` + updateIDText + `"}`,
			wantLocation: updatesURL + "/" + updateIDText,
			wantCalls:    []string{`RequestUpdate("EUR", "MXN")`},
			wantLogged:   false,
		},
		{
			name:         "currency codes are passed to the service as they came",
			body:         `{"base":"eur","quote":"mxn"}`,
			requestErr:   nil,
			wantStatus:   http.StatusAccepted,
			wantBody:     `{"update_id":"` + updateIDText + `"}`,
			wantLocation: updatesURL + "/" + updateIDText,
			wantCalls:    []string{`RequestUpdate("eur", "mxn")`},
			wantLogged:   false,
		},
		{
			name:         "invalid currency",
			body:         `{"base":"EURO","quote":"MXN"}`,
			requestErr:   errInvalidBase,
			wantStatus:   http.StatusBadRequest,
			wantBody:     `{"error":{"code":"invalid_currency","message":"base currency: invalid currency code: \"EURO\""}}`,
			wantLocation: "",
			wantCalls:    []string{`RequestUpdate("EURO", "MXN")`},
			wantLogged:   false,
		},
		{
			name:         "missing field reaches the service as an empty code",
			body:         `{"base":"EUR"}`,
			requestErr:   errInvalidQuote,
			wantStatus:   http.StatusBadRequest,
			wantBody:     `{"error":{"code":"invalid_currency","message":"quote currency: invalid currency code: \"\""}}`,
			wantLocation: "",
			wantCalls:    []string{`RequestUpdate("EUR", "")`},
			wantLogged:   false,
		},
		{
			name:         "same base and quote currency",
			body:         `{"base":"EUR","quote":"EUR"}`,
			requestErr:   errSamePair,
			wantStatus:   http.StatusBadRequest,
			wantBody:     `{"error":{"code":"invalid_pair","message":"currency pair: base and quote currencies must differ: EUR"}}`,
			wantLocation: "",
			wantCalls:    []string{`RequestUpdate("EUR", "EUR")`},
			wantLogged:   false,
		},
		{
			name:         "unsupported currency",
			body:         `{"base":"EUR","quote":"JPY"}`,
			requestErr:   errUnsupported,
			wantStatus:   http.StatusBadRequest,
			wantBody:     `{"error":{"code":"unsupported_currency","message":"currency is not supported: EUR/JPY"}}`,
			wantLocation: "",
			wantCalls:    []string{`RequestUpdate("EUR", "JPY")`},
			wantLogged:   false,
		},
		{
			name:         "body is not json",
			body:         `EUR/MXN`,
			requestErr:   nil,
			wantStatus:   http.StatusBadRequest,
			wantBody:     malformedBody,
			wantLocation: "",
			wantCalls:    nil,
			wantLogged:   false,
		},
		{
			name:         "body is empty",
			body:         ``,
			requestErr:   nil,
			wantStatus:   http.StatusBadRequest,
			wantBody:     malformedBody,
			wantLocation: "",
			wantCalls:    nil,
			wantLogged:   false,
		},
		{
			name:         "field has a wrong type",
			body:         `{"base":1,"quote":2}`,
			requestErr:   nil,
			wantStatus:   http.StatusBadRequest,
			wantBody:     malformedBody,
			wantLocation: "",
			wantCalls:    nil,
			wantLogged:   false,
		},
		{
			name:         "unknown field",
			body:         `{"base":"EUR","quote":"MXN","amount":100}`,
			requestErr:   nil,
			wantStatus:   http.StatusBadRequest,
			wantBody:     malformedBody,
			wantLocation: "",
			wantCalls:    nil,
			wantLogged:   false,
		},
		{
			name:         "data after the json object",
			body:         `{"base":"EUR","quote":"MXN"}{"base":"USD","quote":"MXN"}`,
			requestErr:   nil,
			wantStatus:   http.StatusBadRequest,
			wantBody:     malformedBody,
			wantLocation: "",
			wantCalls:    nil,
			wantLogged:   false,
		},
		{
			name:         "body is too large",
			body:         `{"base":"` + strings.Repeat("E", 2048) + `","quote":"MXN"}`,
			requestErr:   nil,
			wantStatus:   http.StatusBadRequest,
			wantBody:     malformedBody,
			wantLocation: "",
			wantCalls:    nil,
			wantLogged:   false,
		},
		{
			name:         "service fails: details are logged, not returned",
			body:         `{"base":"EUR","quote":"MXN"}`,
			requestErr:   errDatabase,
			wantStatus:   http.StatusInternalServerError,
			wantBody:     internalErrorBody,
			wantLocation: "",
			wantCalls:    []string{`RequestUpdate("EUR", "MXN")`},
			wantLogged:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &stubService{updateID: updateID, requestErr: tt.requestErr}

			got, logs := serve(t, svc, http.MethodPost, updatesURL, tt.body)
			assert.Equal(t, tt.wantStatus, got.Code)
			assert.JSONEq(t, tt.wantBody, got.Body.String())
			assert.Equal(t, tt.wantLocation, got.Header().Get("Location"))
			assert.Equal(t, tt.wantCalls, svc.calls)
			assert.Equal(t, tt.wantLogged, strings.Contains(logs, errDatabase.Error()))
		})
	}
}

func TestHandler_GetUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		id         string
		result     service.UpdateResult
		getErr     error
		wantStatus int
		wantBody   string
		wantCalls  []string
		wantLogged bool
	}{
		{
			name:       "pending update has no price yet",
			id:         updateIDText,
			result:     pendingResult(),
			getErr:     nil,
			wantStatus: http.StatusOK,
			wantBody:   `{"update_id":"` + updateIDText + `","base":"EUR","quote":"MXN","status":"pending"}`,
			wantCalls:  []string{"GetUpdate(" + updateIDText + ")"},
			wantLogged: false,
		},
		{
			name:       "completed update carries the price and the time it was obtained",
			id:         updateIDText,
			result:     completedResult(),
			getErr:     nil,
			wantStatus: http.StatusOK,
			wantBody: `{"update_id":"` + updateIDText + `","base":"EUR","quote":"MXN","status":"completed",` +
				`"price":"20.5806","updated_at":"2026-10-04T12:00:02Z"}`,
			wantCalls:  []string{"GetUpdate(" + updateIDText + ")"},
			wantLogged: false,
		},
		{
			name:       "failed update hides the internal reason",
			id:         updateIDText,
			result:     failedResult(),
			getErr:     nil,
			wantStatus: http.StatusOK,
			wantBody: `{"update_id":"` + updateIDText + `","base":"EUR","quote":"MXN","status":"failed",` +
				`"error":"the quote could not be obtained from the rate provider"}`,
			wantCalls:  []string{"GetUpdate(" + updateIDText + ")"},
			wantLogged: false,
		},
		{
			name:       "identifier is not a uuid",
			id:         "42",
			result:     service.UpdateResult{},
			getErr:     nil,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_id","message":"invalid identifier: \"42\""}}`,
			wantCalls:  nil,
			wantLogged: false,
		},
		{
			name:       "unknown update",
			id:         updateIDText,
			result:     service.UpdateResult{},
			getErr:     errNoUpdate,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"update_not_found","message":"update request not found"}}`,
			wantCalls:  []string{"GetUpdate(" + updateIDText + ")"},
			wantLogged: false,
		},
		{
			name:       "service fails: details are logged, not returned",
			id:         updateIDText,
			result:     service.UpdateResult{},
			getErr:     errDatabase,
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
			wantCalls:  []string{"GetUpdate(" + updateIDText + ")"},
			wantLogged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &stubService{result: tt.result, getErr: tt.getErr}

			got, logs := serve(t, svc, http.MethodGet, updatesURL+"/"+tt.id, "")
			assert.Equal(t, tt.wantStatus, got.Code)
			assert.JSONEq(t, tt.wantBody, got.Body.String())
			assert.Equal(t, tt.wantCalls, svc.calls)
			assert.Equal(t, tt.wantLogged, strings.Contains(logs, errDatabase.Error()))
		})
	}
}

func TestHandler_GetLatestQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		latest     domain.Quote
		latestErr  error
		wantStatus int
		wantBody   string
		wantCalls  []string
		wantLogged bool
	}{
		{
			name:       "latest quote is returned",
			query:      "?base=EUR&quote=MXN",
			latest:     eurMXNQuote(),
			latestErr:  nil,
			wantStatus: http.StatusOK,
			wantBody:   `{"base":"EUR","quote":"MXN","price":"20.5806","updated_at":"2026-10-04T12:00:02Z"}`,
			wantCalls:  []string{`GetLatestQuote("EUR", "MXN")`},
			wantLogged: false,
		},
		{
			name:       "currency codes are passed to the service as they came",
			query:      "?base=eur&quote=mxn",
			latest:     eurMXNQuote(),
			latestErr:  nil,
			wantStatus: http.StatusOK,
			wantBody:   `{"base":"EUR","quote":"MXN","price":"20.5806","updated_at":"2026-10-04T12:00:02Z"}`,
			wantCalls:  []string{`GetLatestQuote("eur", "mxn")`},
			wantLogged: false,
		},
		{
			name:       "missing parameter reaches the service as an empty code",
			query:      "?base=EUR",
			latest:     domain.Quote{},
			latestErr:  errInvalidQuote,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_currency","message":"quote currency: invalid currency code: \"\""}}`,
			wantCalls:  []string{`GetLatestQuote("EUR", "")`},
			wantLogged: false,
		},
		{
			name:       "same base and quote currency",
			query:      "?base=EUR&quote=EUR",
			latest:     domain.Quote{},
			latestErr:  errSamePair,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_pair","message":"currency pair: base and quote currencies must differ: EUR"}}`,
			wantCalls:  []string{`GetLatestQuote("EUR", "EUR")`},
			wantLogged: false,
		},
		{
			name:       "unsupported currency",
			query:      "?base=EUR&quote=JPY",
			latest:     domain.Quote{},
			latestErr:  errUnsupported,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"unsupported_currency","message":"currency is not supported: EUR/JPY"}}`,
			wantCalls:  []string{`GetLatestQuote("EUR", "JPY")`},
			wantLogged: false,
		},
		{
			name:       "pair has no quote yet",
			query:      "?base=EUR&quote=MXN",
			latest:     domain.Quote{},
			latestErr:  errNoQuote,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"quote_not_found","message":"the pair has no quote yet"}}`,
			wantCalls:  []string{`GetLatestQuote("EUR", "MXN")`},
			wantLogged: false,
		},
		{
			name:       "service fails: details are logged, not returned",
			query:      "?base=EUR&quote=MXN",
			latest:     domain.Quote{},
			latestErr:  errDatabase,
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
			wantCalls:  []string{`GetLatestQuote("EUR", "MXN")`},
			wantLogged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &stubService{latest: tt.latest, latestErr: tt.latestErr}

			got, logs := serve(t, svc, http.MethodGet, latestURL+tt.query, "")
			assert.Equal(t, tt.wantStatus, got.Code)
			assert.JSONEq(t, tt.wantBody, got.Body.String())
			assert.Equal(t, tt.wantCalls, svc.calls)
			assert.Equal(t, tt.wantLogged, strings.Contains(logs, errDatabase.Error()))
		})
	}
}

func TestHandler_Routes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{
			name:       "health check",
			method:     http.MethodGet,
			target:     "/healthz",
			wantStatus: http.StatusOK,
		},
		{
			name:       "updates cannot be listed",
			method:     http.MethodGet,
			target:     updatesURL,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "an update cannot be deleted",
			method:     http.MethodDelete,
			target:     updatesURL + "/" + updateIDText,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "latest quote cannot be posted",
			method:     http.MethodPost,
			target:     latestURL,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "update without an identifier",
			method:     http.MethodGet,
			target:     updatesURL + "/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown path",
			method:     http.MethodGet,
			target:     "/api/v2/quotes",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, _ := serve(t, &stubService{}, tt.method, tt.target, "")
			assert.Equal(t, tt.wantStatus, got.Code)
		})
	}
}

func TestHandler_ResponsesAreJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{
			name:   "success",
			method: http.MethodPost,
			target: updatesURL,
			body:   `{"base":"EUR","quote":"MXN"}`,
		},
		{
			name:   "error",
			method: http.MethodGet,
			target: updatesURL + "/42",
			body:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, _ := serve(t, &stubService{updateID: updateID}, tt.method, tt.target, tt.body)
			assert.Equal(t, "application/json; charset=utf-8", got.Header().Get("Content-Type"))
		})
	}
}
