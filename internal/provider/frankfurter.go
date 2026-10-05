// Package provider contains adapters for external exchange rate sources.
// They implement the RateProvider port declared by the service package.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/shopspring/decimal"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

var _ service.RateProvider = (*Frankfurter)(nil)

// maxResponseBytes caps how much of a response body is read. A rate response
// is well under a kilobyte; the cap protects against a misbehaving server.
const maxResponseBytes = 1 << 20

// Frankfurter fetches exchange rates from the Frankfurter API
// (https://frankfurter.dev), which publishes the reference rates of the
// European Central Bank. The API needs no key.
//
// Frankfurter is safe for concurrent use.
type Frankfurter struct {
	baseURL *url.URL
	client  *http.Client
}

// latestRates is the part of the "latest" response the adapter relies on:
//
//	{"amount":1.0,"base":"EUR","date":"2026-10-02","rates":{"MXN":20.5806}}
//
// Rates are decoded straight into decimals, so they never pass through a
// float64 and keep every digit the provider sent.
type latestRates struct {
	Base  string                     `json:"base"`
	Rates map[string]decimal.Decimal `json:"rates"`
}

// NewFrankfurter returns a Frankfurter adapter that sends requests to
// baseURL, e.g. "https://api.frankfurter.dev/v1", using client. The timeout
// of a request is whatever client and the request context allow.
//
// It returns [ErrInvalidBaseURL] if baseURL is not an absolute http(s) URL.
func NewFrankfurter(baseURL string, client *http.Client) (*Frankfurter, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidBaseURL, err)
	}

	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidBaseURL, baseURL)
	}

	return &Frankfurter{baseURL: parsed, client: client}, nil
}

// Rate returns the price of one unit of pair.Base expressed in pair.Quote.
//
// It returns [ErrUnexpectedStatus], [ErrMalformedResponse] or
// [ErrRateMissing] if the provider does not deliver a usable rate, and the
// underlying transport or context error if the request itself fails.
func (p *Frankfurter) Rate(ctx context.Context, pair domain.Pair) (decimal.Decimal, error) {
	endpoint := p.baseURL.JoinPath("latest")
	endpoint.RawQuery = url.Values{
		"base":    {pair.Base.String()},
		"symbols": {pair.Quote.String()},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("frankfurter: build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("frankfurter: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return decimal.Decimal{}, fmt.Errorf("frankfurter: %w: %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	var payload latestRates
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&payload); err != nil {
		return decimal.Decimal{}, fmt.Errorf("frankfurter: %w: %w", ErrMalformedResponse, err)
	}

	if payload.Base != pair.Base.String() {
		return decimal.Decimal{}, fmt.Errorf(
			"frankfurter: %w: rates are for base %q, want %q", ErrMalformedResponse, payload.Base, pair.Base,
		)
	}

	rate, ok := payload.Rates[pair.Quote.String()]
	if !ok {
		return decimal.Decimal{}, fmt.Errorf("frankfurter: %w: %s", ErrRateMissing, pair.Quote)
	}

	return rate, nil
}
