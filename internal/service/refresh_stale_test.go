package service_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

// maxAge is how old a quote may get before the tests expect it to be refreshed.
const maxAge = time.Hour

var mxnEUR = domain.Pair{Base: "MXN", Quote: "EUR"}

// quoteAged is a quote of pair obtained the given time before now.
func quoteAged(pair domain.Pair, age time.Duration) domain.Quote {
	return domain.Quote{
		ID:         quoteID,
		Pair:       pair,
		Price:      price,
		ObtainedAt: now.Add(-age),
	}
}

// pairsOf lists the pairs of the given requests, in order.
func pairsOf(reqs []domain.UpdateRequest) []domain.Pair {
	var pairs []domain.Pair
	for _, req := range reqs {
		pairs = append(pairs, req.Pair)
	}

	return pairs
}

func TestQuoteService_RefreshStale(t *testing.T) {
	t.Parallel()

	var (
		eurAndMXN  = stubCurrencies{all: []domain.Currency{"EUR", "MXN"}, allErr: nil}
		workingIDs = stubIDs{updateID: updateID, err: nil}
		noQuotes   = map[domain.Pair]domain.Quote{}
	)

	tests := []struct {
		name          string
		currencies    stubCurrencies
		latest        map[domain.Pair]domain.Quote
		latestErr     error
		ids           stubIDs
		createErr     error
		want          int
		wantRequested []domain.Pair
		wantErr       error
	}{
		{
			name:       "no quotes yet: every pair is requested in both directions",
			currencies: stubCurrencies{all: []domain.Currency{"EUR", "MXN", "USD"}, allErr: nil},
			latest:     noQuotes,
			latestErr:  nil,
			ids:        workingIDs,
			createErr:  nil,
			want:       6,
			wantRequested: []domain.Pair{
				{Base: "EUR", Quote: "MXN"},
				{Base: "EUR", Quote: "USD"},
				{Base: "MXN", Quote: "EUR"},
				{Base: "MXN", Quote: "USD"},
				{Base: "USD", Quote: "EUR"},
				{Base: "USD", Quote: "MXN"},
			},
			wantErr: nil,
		},
		{
			name:       "fresh quotes: nothing is requested",
			currencies: eurAndMXN,
			latest: map[domain.Pair]domain.Quote{
				eurMXN: quoteAged(eurMXN, 10*time.Minute),
				mxnEUR: quoteAged(mxnEUR, 59*time.Minute),
			},
			latestErr:     nil,
			ids:           workingIDs,
			createErr:     nil,
			want:          0,
			wantRequested: nil,
			wantErr:       nil,
		},
		{
			name:       "stale quotes are requested",
			currencies: eurAndMXN,
			latest: map[domain.Pair]domain.Quote{
				eurMXN: quoteAged(eurMXN, 2*time.Hour),
				mxnEUR: quoteAged(mxnEUR, 48*time.Hour),
			},
			latestErr:     nil,
			ids:           workingIDs,
			createErr:     nil,
			want:          2,
			wantRequested: []domain.Pair{eurMXN, mxnEUR},
			wantErr:       nil,
		},
		{
			name:       "a quote exactly max age old is stale",
			currencies: eurAndMXN,
			latest: map[domain.Pair]domain.Quote{
				eurMXN: quoteAged(eurMXN, maxAge),
				mxnEUR: quoteAged(mxnEUR, maxAge-time.Second),
			},
			latestErr:     nil,
			ids:           workingIDs,
			createErr:     nil,
			want:          1,
			wantRequested: []domain.Pair{eurMXN},
			wantErr:       nil,
		},
		{
			name:       "each direction of a pair is judged on its own",
			currencies: eurAndMXN,
			latest: map[domain.Pair]domain.Quote{
				eurMXN: quoteAged(eurMXN, 10*time.Minute),
			},
			latestErr:     nil,
			ids:           workingIDs,
			createErr:     nil,
			want:          1,
			wantRequested: []domain.Pair{mxnEUR},
			wantErr:       nil,
		},
		{
			name:          "a single currency makes no pairs",
			currencies:    stubCurrencies{all: []domain.Currency{"EUR"}, allErr: nil},
			latest:        noQuotes,
			latestErr:     nil,
			ids:           workingIDs,
			createErr:     nil,
			want:          0,
			wantRequested: nil,
			wantErr:       nil,
		},
		{
			name:          "no supported currencies",
			currencies:    stubCurrencies{all: nil, allErr: nil},
			latest:        noQuotes,
			latestErr:     nil,
			ids:           workingIDs,
			createErr:     nil,
			want:          0,
			wantRequested: nil,
			wantErr:       nil,
		},
		{
			name:          "listing currencies fails",
			currencies:    stubCurrencies{all: nil, allErr: errStorage},
			latest:        noQuotes,
			latestErr:     nil,
			ids:           workingIDs,
			createErr:     nil,
			want:          0,
			wantRequested: nil,
			wantErr:       errStorage,
		},
		{
			name:          "reading the latest quote fails: nothing is requested blindly",
			currencies:    eurAndMXN,
			latest:        noQuotes,
			latestErr:     errStorage,
			ids:           workingIDs,
			createErr:     nil,
			want:          0,
			wantRequested: nil,
			wantErr:       errStorage,
		},
		{
			name:          "id generation fails",
			currencies:    eurAndMXN,
			latest:        noQuotes,
			latestErr:     nil,
			ids:           stubIDs{updateID: domain.UpdateID{}, err: errIDs},
			createErr:     nil,
			want:          0,
			wantRequested: nil,
			wantErr:       errIDs,
		},
		{
			name:          "storing a request fails: the other pairs are still tried",
			currencies:    eurAndMXN,
			latest:        noQuotes,
			latestErr:     nil,
			ids:           workingIDs,
			createErr:     errStorage,
			want:          0,
			wantRequested: []domain.Pair{eurMXN, mxnEUR},
			wantErr:       errStorage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updates := &stubUpdates{createErr: tt.createErr}
			quotes := &stubQuotes{latestByPair: tt.latest, latestErr: tt.latestErr}
			svc := service.NewQuoteService(updates, quotes, tt.currencies, fixedClock{now: now}, tt.ids)

			got, err := svc.RefreshStale(t.Context(), maxAge)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantRequested, pairsOf(updates.createdAll))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestQuoteService_RefreshStale_StoresOrdinaryPendingRequests(t *testing.T) {
	t.Parallel()

	updates := &stubUpdates{}
	quotes := &stubQuotes{latestByPair: map[domain.Pair]domain.Quote{mxnEUR: quoteAged(mxnEUR, time.Minute)}}
	currencies := stubCurrencies{all: []domain.Currency{"EUR", "MXN"}}
	svc := service.NewQuoteService(updates, quotes, currencies, fixedClock{now: now}, stubIDs{updateID: updateID})

	_, err := svc.RefreshStale(t.Context(), maxAge)
	require.NoError(t, err)

	// The same request a client would create: the worker cannot tell them apart.
	assert.Equal(t, []domain.UpdateRequest{pendingRequest(updateID)}, updates.createdAll)
}
