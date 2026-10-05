package handler

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

// failedUpdateMessage is what a client is told about a failed update. The
// actual reason is stored with the request and may contain internal details
// such as the provider address, so it is not exposed.
const failedUpdateMessage = "the quote could not be obtained from the rate provider"

// requestUpdateRequest is the body of POST /api/v1/quotes/updates.
type requestUpdateRequest struct {
	Base  string `json:"base"`
	Quote string `json:"quote"`
}

// requestUpdateResponse is the body of a successful POST /api/v1/quotes/updates.
type requestUpdateResponse struct {
	UpdateID string `json:"update_id"`
}

// updateResponse is the body of GET /api/v1/quotes/updates/{id}.
//
// Price and UpdatedAt are present once the update is completed, Error once
// it has failed. Price is a JSON string so that clients do not lose
// precision by parsing it as a floating-point number.
type updateResponse struct {
	UpdateID  string           `json:"update_id"`
	Base      string           `json:"base"`
	Quote     string           `json:"quote"`
	Status    string           `json:"status"`
	Price     *decimal.Decimal `json:"price,omitempty"`
	UpdatedAt *time.Time       `json:"updated_at,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// quoteResponse is the body of GET /api/v1/quotes/latest.
type quoteResponse struct {
	Base      string          `json:"base"`
	Quote     string          `json:"quote"`
	Price     decimal.Decimal `json:"price"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// errorResponse is the body of every error response.
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newUpdateResponse(result service.UpdateResult) updateResponse {
	resp := updateResponse{
		UpdateID:  result.Request.ID.String(),
		Base:      result.Request.Pair.Base.String(),
		Quote:     result.Request.Pair.Quote.String(),
		Status:    string(result.Request.Status),
		Price:     nil,
		UpdatedAt: nil,
		Error:     "",
	}

	if result.Quote != nil {
		resp.Price = &result.Quote.Price
		resp.UpdatedAt = &result.Quote.ObtainedAt
	}

	if result.Request.Status == domain.UpdateFailed {
		resp.Error = failedUpdateMessage
	}

	return resp
}

func newQuoteResponse(quote domain.Quote) quoteResponse {
	return quoteResponse{
		Base:      quote.Pair.Base.String(),
		Quote:     quote.Pair.Quote.String(),
		Price:     quote.Price,
		UpdatedAt: quote.ObtainedAt,
	}
}
