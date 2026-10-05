package handler

import (
	"errors"
	"net/http"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
)

// errMalformedRequest is returned when a request body cannot be decoded.
var errMalformedRequest = errors.New("malformed request body")

// Machine-readable error codes of the API.
const (
	codeMalformedRequest    = "malformed_request"
	codeInvalidCurrency     = "invalid_currency"
	codeInvalidPair         = "invalid_pair"
	codeUnsupportedCurrency = "unsupported_currency"
	codeInvalidID           = "invalid_id"
	codeUpdateNotFound      = "update_not_found"
	codeQuoteNotFound       = "quote_not_found"
	codeInternal            = "internal_error"
)

// apiError is how an error is presented to an API client.
type apiError struct {
	status  int
	code    string
	message string
}

// describe translates an error of the service into an API error.
//
// Validation errors describe the client's own input, so their text is passed
// on. Everything unrecognised becomes a 500 with a fixed message: internal
// details must not leak to clients.
func describe(err error) apiError {
	switch {
	case errors.Is(err, errMalformedRequest):
		return apiError{
			status:  http.StatusBadRequest,
			code:    codeMalformedRequest,
			message: `request body must be a JSON object with "base" and "quote" fields`,
		}
	case errors.Is(err, domain.ErrInvalidCurrency):
		return apiError{status: http.StatusBadRequest, code: codeInvalidCurrency, message: err.Error()}
	case errors.Is(err, domain.ErrInvalidPair):
		return apiError{status: http.StatusBadRequest, code: codeInvalidPair, message: err.Error()}
	case errors.Is(err, domain.ErrUnsupportedCurrency):
		return apiError{status: http.StatusBadRequest, code: codeUnsupportedCurrency, message: err.Error()}
	case errors.Is(err, domain.ErrInvalidID):
		return apiError{status: http.StatusBadRequest, code: codeInvalidID, message: err.Error()}
	case errors.Is(err, domain.ErrUpdateNotFound):
		return apiError{status: http.StatusNotFound, code: codeUpdateNotFound, message: "update request not found"}
	case errors.Is(err, domain.ErrQuoteNotFound):
		return apiError{status: http.StatusNotFound, code: codeQuoteNotFound, message: "the pair has no quote yet"}
	default:
		return apiError{status: http.StatusInternalServerError, code: codeInternal, message: "internal server error"}
	}
}
