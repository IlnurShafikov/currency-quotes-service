-- +goose Up
CREATE TABLE currencies (
    code       VARCHAR(10) PRIMARY KEY CHECK (code ~ '^[A-Z0-9]{2,10}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE quotes (
    id             UUID PRIMARY KEY,
    base_currency  VARCHAR(10) NOT NULL REFERENCES currencies (code),
    quote_currency VARCHAR(10) NOT NULL REFERENCES currencies (code),
    price          NUMERIC(38, 18) NOT NULL CHECK (price > 0),
    obtained_at    TIMESTAMPTZ NOT NULL,

    CHECK (base_currency <> quote_currency)
);

CREATE INDEX quotes_latest_idx
    ON quotes (base_currency, quote_currency, obtained_at DESC);

CREATE TABLE quote_update_requests (
    id             UUID PRIMARY KEY,
    base_currency  VARCHAR(10) NOT NULL REFERENCES currencies (code),
    quote_currency VARCHAR(10) NOT NULL REFERENCES currencies (code),
    status         TEXT NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending', 'completed', 'failed')),
    quote_id       UUID REFERENCES quotes (id),
    error          TEXT,
    attempts       INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    requested_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,

    CHECK (base_currency <> quote_currency),
    CHECK ((status = 'completed') = (quote_id IS NOT NULL)),
    CHECK ((status = 'failed') = (error IS NOT NULL)),
    CHECK ((status = 'pending') = (finished_at IS NULL))
);

-- Idempotency: at most one active (pending) request per currency pair.
CREATE UNIQUE INDEX quote_update_requests_active_pair_uq
    ON quote_update_requests (base_currency, quote_currency)
    WHERE status = 'pending';

-- Worker queue: pending requests in arrival order.
CREATE INDEX quote_update_requests_pending_idx
    ON quote_update_requests (requested_at)
    WHERE status = 'pending';

-- +goose Down
DROP TABLE quote_update_requests;
DROP TABLE quotes;
DROP TABLE currencies;