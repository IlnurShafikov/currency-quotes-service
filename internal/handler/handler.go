// Package handler is the HTTP adapter of the service. It translates HTTP
// requests into calls to the service layer and its results, including
// errors, into JSON responses.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/IlnurShafikov/currency-quotes-service/internal/domain"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
)

// maxBodyBytes caps the size of a request body. The only body the API
// accepts is a small JSON object with two currency codes.
const maxBodyBytes = 1 << 10

// updatesPath is the collection of update requests.
const updatesPath = "/api/v1/quotes/updates"

// QuoteService is what the handler needs from the service layer.
type QuoteService interface {
	RequestUpdate(ctx context.Context, base, quote string) (domain.UpdateID, error)
	GetUpdate(ctx context.Context, id domain.UpdateID) (service.UpdateResult, error)
	GetLatestQuote(ctx context.Context, base, quote string) (domain.Quote, error)
}

// Handler serves the HTTP API of the service.
type Handler struct {
	quotes QuoteService
	log    *slog.Logger
}

// New returns a Handler that serves requests using quotes and reports
// unexpected errors to log.
func New(quotes QuoteService, log *slog.Logger) *Handler {
	return &Handler{quotes: quotes, log: log}
}

// Routes returns the HTTP routes of the API.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST "+updatesPath, h.requestUpdate)
	mux.HandleFunc("GET "+updatesPath+"/{id}", h.getUpdate)
	mux.HandleFunc("GET /api/v1/quotes/latest", h.getLatestQuote)
	mux.HandleFunc("GET /healthz", h.health)

	return mux
}

// requestUpdate registers a quote update and answers 202 Accepted with its
// identifier: the update itself is carried out in the background.
func (h *Handler) requestUpdate(w http.ResponseWriter, r *http.Request) {
	var body requestUpdateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)

		return
	}

	id, err := h.quotes.RequestUpdate(r.Context(), body.Base, body.Quote)
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	w.Header().Set("Location", updatesPath+"/"+id.String())
	h.writeJSON(w, r, http.StatusAccepted, requestUpdateResponse{UpdateID: id.String()})
}

// getUpdate answers with the state of an update request and, once it is
// completed, the price and the time it was obtained.
func (h *Handler) getUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := domain.ParseUpdateID(r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	result, err := h.quotes.GetUpdate(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	h.writeJSON(w, r, http.StatusOK, newUpdateResponse(result))
}

// getLatestQuote answers with the latest quote of the pair given by the
// "base" and "quote" query parameters.
func (h *Handler) getLatestQuote(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	quote, err := h.quotes.GetLatestQuote(r.Context(), query.Get("base"), query.Get("quote"))
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	h.writeJSON(w, r, http.StatusOK, newQuoteResponse(quote))
}

// health reports that the process is up and able to serve requests.
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

// writeError answers with the API representation of err. Errors the client
// is not responsible for are logged, since the client only sees a generic
// message.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	described := describe(err)

	if described.status >= http.StatusInternalServerError {
		h.log.ErrorContext(r.Context(), "request failed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Any("error", err),
		)
	}

	h.writeJSON(w, r, described.status, errorResponse{
		Error: errorBody{Code: described.code, Message: described.message},
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent; all that is left is to record it.
		h.log.ErrorContext(r.Context(), "write response", slog.Any("error", err))
	}
}

// decodeJSON reads a JSON object from the request body into dst. It rejects
// bodies that are too large, contain unknown fields or carry anything after
// the object. It returns errMalformedRequest on any of those.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("%w: %w", errMalformedRequest, err)
	}

	if decoder.More() {
		return fmt.Errorf("%w: unexpected data after the JSON object", errMalformedRequest)
	}

	return nil
}
