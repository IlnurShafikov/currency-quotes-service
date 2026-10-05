package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

func TestQuoteService_RequestUpdate(t *testing.T) {
	t.Parallel()

	var (
		supported   = stubCurrencies{supported: true, err: nil}
		unsupported = stubCurrencies{supported: false, err: nil}
		workingIDs  = stubIDs{updateID: updateID, err: nil}
		noRequest   = domain.UpdateRequest{}
	)

	tests := []struct {
		name        string
		base        string
		quote       string
		currencies  stubCurrencies
		ids         stubIDs
		stored      domain.UpdateRequest
		createErr   error
		want        domain.UpdateID
		wantCreated domain.UpdateRequest
		wantErr     error
	}{
		{
			name:        "new request is created",
			base:        "EUR",
			quote:       "MXN",
			currencies:  supported,
			ids:         workingIDs,
			stored:      pendingRequest(updateID),
			createErr:   nil,
			want:        updateID,
			wantCreated: pendingRequest(updateID),
			wantErr:     nil,
		},
		{
			name:        "pending request for the pair is reused",
			base:        "EUR",
			quote:       "MXN",
			currencies:  supported,
			ids:         workingIDs,
			stored:      pendingRequest(existingID),
			createErr:   nil,
			want:        existingID,
			wantCreated: pendingRequest(updateID),
			wantErr:     nil,
		},
		{
			name:        "currency codes are normalised",
			base:        "eur",
			quote:       "mxn",
			currencies:  supported,
			ids:         workingIDs,
			stored:      pendingRequest(updateID),
			createErr:   nil,
			want:        updateID,
			wantCreated: pendingRequest(updateID),
			wantErr:     nil,
		},
		{
			name:        "invalid base currency",
			base:        "EURO",
			quote:       "MXN",
			currencies:  supported,
			ids:         workingIDs,
			stored:      noRequest,
			createErr:   nil,
			want:        domain.UpdateID{},
			wantCreated: noRequest,
			wantErr:     domain.ErrInvalidCurrency,
		},
		{
			name:        "invalid quote currency",
			base:        "EUR",
			quote:       "",
			currencies:  supported,
			ids:         workingIDs,
			stored:      noRequest,
			createErr:   nil,
			want:        domain.UpdateID{},
			wantCreated: noRequest,
			wantErr:     domain.ErrInvalidCurrency,
		},
		{
			name:        "same base and quote currency",
			base:        "EUR",
			quote:       "EUR",
			currencies:  supported,
			ids:         workingIDs,
			stored:      noRequest,
			createErr:   nil,
			want:        domain.UpdateID{},
			wantCreated: noRequest,
			wantErr:     domain.ErrInvalidPair,
		},
		{
			name:        "unsupported currency",
			base:        "EUR",
			quote:       "JPY",
			currencies:  unsupported,
			ids:         workingIDs,
			stored:      noRequest,
			createErr:   nil,
			want:        domain.UpdateID{},
			wantCreated: noRequest,
			wantErr:     domain.ErrUnsupportedCurrency,
		},
		{
			name:        "currency lookup fails",
			base:        "EUR",
			quote:       "MXN",
			currencies:  stubCurrencies{supported: false, err: errStorage},
			ids:         workingIDs,
			stored:      noRequest,
			createErr:   nil,
			want:        domain.UpdateID{},
			wantCreated: noRequest,
			wantErr:     errStorage,
		},
		{
			name:        "id generation fails",
			base:        "EUR",
			quote:       "MXN",
			currencies:  supported,
			ids:         stubIDs{updateID: domain.UpdateID{}, err: errIDs},
			stored:      noRequest,
			createErr:   nil,
			want:        domain.UpdateID{},
			wantCreated: noRequest,
			wantErr:     errIDs,
		},
		{
			name:        "storing the request fails",
			base:        "EUR",
			quote:       "MXN",
			currencies:  supported,
			ids:         workingIDs,
			stored:      noRequest,
			createErr:   errStorage,
			want:        domain.UpdateID{},
			wantCreated: pendingRequest(updateID),
			wantErr:     errStorage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updates := &stubUpdates{stored: tt.stored, createErr: tt.createErr}
			svc := service.NewQuoteService(updates, &stubQuotes{}, tt.currencies, fixedClock{now: now}, tt.ids)

			got, err := svc.RequestUpdate(t.Context(), tt.base, tt.quote)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCreated, updates.created)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestQuoteService_GetUpdate(t *testing.T) {
	t.Parallel()

	var (
		noRequest = domain.UpdateRequest{}
		noQuote   = domain.Quote{}
		noQuoteID = domain.QuoteID{}
	)

	tests := []struct {
		name            string
		found           domain.UpdateRequest
		getErr          error
		quote           domain.Quote
		quoteErr        error
		want            service.UpdateResult
		wantQuoteLookup domain.QuoteID
		wantErr         error
	}{
		{
			name:            "pending request has no quote yet",
			found:           pendingRequest(updateID),
			getErr:          nil,
			quote:           noQuote,
			quoteErr:        nil,
			want:            service.UpdateResult{Request: pendingRequest(updateID), Quote: nil},
			wantQuoteLookup: noQuoteID,
			wantErr:         nil,
		},
		{
			name:            "completed request comes with its quote",
			found:           completedRequest(),
			getErr:          nil,
			quote:           eurMXNQuote(),
			quoteErr:        nil,
			want:            service.UpdateResult{Request: completedRequest(), Quote: new(eurMXNQuote())},
			wantQuoteLookup: quoteID,
			wantErr:         nil,
		},
		{
			name:            "failed request has no quote",
			found:           failedRequest(),
			getErr:          nil,
			quote:           noQuote,
			quoteErr:        nil,
			want:            service.UpdateResult{Request: failedRequest(), Quote: nil},
			wantQuoteLookup: noQuoteID,
			wantErr:         nil,
		},
		{
			name:            "unknown request",
			found:           noRequest,
			getErr:          domain.ErrUpdateNotFound,
			quote:           noQuote,
			quoteErr:        nil,
			want:            service.UpdateResult{Request: noRequest, Quote: nil},
			wantQuoteLookup: noQuoteID,
			wantErr:         domain.ErrUpdateNotFound,
		},
		{
			name:            "loading the request fails",
			found:           noRequest,
			getErr:          errStorage,
			quote:           noQuote,
			quoteErr:        nil,
			want:            service.UpdateResult{Request: noRequest, Quote: nil},
			wantQuoteLookup: noQuoteID,
			wantErr:         errStorage,
		},
		{
			name:            "loading the quote fails",
			found:           completedRequest(),
			getErr:          nil,
			quote:           noQuote,
			quoteErr:        errStorage,
			want:            service.UpdateResult{Request: noRequest, Quote: nil},
			wantQuoteLookup: quoteID,
			wantErr:         errStorage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updates := &stubUpdates{found: tt.found, getErr: tt.getErr}
			quotes := &stubQuotes{byID: tt.quote, getErr: tt.quoteErr}
			svc := service.NewQuoteService(updates, quotes, stubCurrencies{}, fixedClock{now: now}, stubIDs{})

			got, err := svc.GetUpdate(t.Context(), updateID)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, updateID, updates.gotID)
			assert.Equal(t, tt.wantQuoteLookup, quotes.gotID)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestQuoteService_GetLatestQuote(t *testing.T) {
	t.Parallel()

	var (
		supported   = stubCurrencies{supported: true, err: nil}
		unsupported = stubCurrencies{supported: false, err: nil}
		noQuote     = domain.Quote{}
		noPair      = domain.Pair{}
	)

	tests := []struct {
		name       string
		base       string
		quote      string
		currencies stubCurrencies
		latest     domain.Quote
		latestErr  error
		want       domain.Quote
		wantLookup domain.Pair
		wantErr    error
	}{
		{
			name:       "latest quote is returned",
			base:       "EUR",
			quote:      "MXN",
			currencies: supported,
			latest:     eurMXNQuote(),
			latestErr:  nil,
			want:       eurMXNQuote(),
			wantLookup: eurMXN,
			wantErr:    nil,
		},
		{
			name:       "currency codes are normalised",
			base:       "eur",
			quote:      "mxn",
			currencies: supported,
			latest:     eurMXNQuote(),
			latestErr:  nil,
			want:       eurMXNQuote(),
			wantLookup: eurMXN,
			wantErr:    nil,
		},
		{
			name:       "pair has no quotes yet",
			base:       "EUR",
			quote:      "MXN",
			currencies: supported,
			latest:     noQuote,
			latestErr:  domain.ErrQuoteNotFound,
			want:       noQuote,
			wantLookup: eurMXN,
			wantErr:    domain.ErrQuoteNotFound,
		},
		{
			name:       "invalid base currency",
			base:       "EURO",
			quote:      "MXN",
			currencies: supported,
			latest:     noQuote,
			latestErr:  nil,
			want:       noQuote,
			wantLookup: noPair,
			wantErr:    domain.ErrInvalidCurrency,
		},
		{
			name:       "same base and quote currency",
			base:       "EUR",
			quote:      "EUR",
			currencies: supported,
			latest:     noQuote,
			latestErr:  nil,
			want:       noQuote,
			wantLookup: noPair,
			wantErr:    domain.ErrInvalidPair,
		},
		{
			name:       "unsupported currency",
			base:       "EUR",
			quote:      "JPY",
			currencies: unsupported,
			latest:     noQuote,
			latestErr:  nil,
			want:       noQuote,
			wantLookup: noPair,
			wantErr:    domain.ErrUnsupportedCurrency,
		},
		{
			name:       "currency lookup fails",
			base:       "EUR",
			quote:      "MXN",
			currencies: stubCurrencies{supported: false, err: errStorage},
			latest:     noQuote,
			latestErr:  nil,
			want:       noQuote,
			wantLookup: noPair,
			wantErr:    errStorage,
		},
		{
			name:       "loading the quote fails",
			base:       "EUR",
			quote:      "MXN",
			currencies: supported,
			latest:     noQuote,
			latestErr:  errStorage,
			want:       noQuote,
			wantLookup: eurMXN,
			wantErr:    errStorage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			quotes := &stubQuotes{latest: tt.latest, latestErr: tt.latestErr}
			svc := service.NewQuoteService(&stubUpdates{}, quotes, tt.currencies, fixedClock{now: now}, stubIDs{})

			got, err := svc.GetLatestQuote(t.Context(), tt.base, tt.quote)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantLookup, quotes.gotPair)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
