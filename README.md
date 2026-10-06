# currency-quotes-service

[![CI](https://github.com/IlnurShafikov/currency-quotes-service/actions/workflows/ci.yml/badge.svg)](https://github.com/IlnurShafikov/currency-quotes-service/actions/workflows/ci.yml)

An asynchronous currency quotes service written in Go.

A client asks the service to update the quote of a currency pair and gets an
identifier back immediately. The service fetches the price from an external
rate provider in the background. The client then reads the result by that
identifier, or reads the latest known quote of the pair.

Supported currencies: `USD`, `EUR`, `MXN`.

## Contents

- [Quick start](#quick-start)
- [API](#api)
- [How it works](#how-it-works)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [Development](#development)
- [Design decisions](#design-decisions)
- [Limitations](#limitations)

## Quick start

The only requirement is Docker.

```bash
docker compose up --build
```

This starts PostgreSQL and the service. The service applies its database
migrations on start-up and listens on <http://localhost:8080>. To use another
host port: `HTTP_PORT=9090 docker compose up --build`.

Request an update:

```bash
curl -i -X POST http://localhost:8080/api/v1/quotes/updates \
  -d '{"base":"EUR","quote":"MXN"}'
```

```
HTTP/1.1 202 Accepted
Location: /api/v1/quotes/updates/01a10c86-d74e-7d7e-bf93-5c4d19c7a773

{"update_id":"01a10c86-d74e-7d7e-bf93-5c4d19c7a773"}
```

Read its result (replace the identifier with the one you received):

```bash
curl http://localhost:8080/api/v1/quotes/updates/01a10c86-d74e-7d7e-bf93-5c4d19c7a773
```

```json
{"update_id":"01a10c86-d74e-7d7e-bf93-5c4d19c7a773","base":"EUR","quote":"MXN","status":"completed","price":"20.3348","updated_at":"2026-10-05T14:45:26.796602Z"}
```

Read the latest quote of the pair:

```bash
curl 'http://localhost:8080/api/v1/quotes/latest?base=EUR&quote=MXN'
```

```json
{"base":"EUR","quote":"MXN","price":"20.3348","updated_at":"2026-10-05T14:45:26.796602Z"}
```

Stop everything and remove the database volume:

```bash
docker compose down -v
```

> On Windows run the `curl` commands in Git Bash, or use `curl.exe` in
> PowerShell, where `curl` is an alias of another command.

## API

The full contract is in [api/openapi.yaml](api/openapi.yaml) (OpenAPI 3.0). Paste
it into <https://editor.swagger.io> to browse it.

| Operation | Request | Success |
|---|---|---|
| Request a quote update | `POST /api/v1/quotes/updates` with `{"base":"EUR","quote":"MXN"}` | `202`, `{"update_id":"..."}` |
| Get an update by id | `GET /api/v1/quotes/updates/{id}` | `200`, status, plus price and time once completed |
| Get the latest quote | `GET /api/v1/quotes/latest?base=EUR&quote=MXN` | `200`, price and time |
| Liveness check | `GET /healthz` | `200` |

An update is in one of three states:

| Status | Meaning | Extra fields |
|---|---|---|
| `pending` | accepted, not carried out yet | none |
| `completed` | the price is obtained | `price`, `updated_at` |
| `failed` | the price could not be obtained after all attempts | `error` |

Errors share one shape:

```json
{"error":{"code":"unsupported_currency","message":"currency is not supported: EUR/JPY"}}
```

| Status | Codes |
|---|---|
| `400` | `malformed_request`, `invalid_currency`, `invalid_pair`, `unsupported_currency`, `invalid_id` |
| `404` | `update_not_found`, `quote_not_found` |
| `500` | `internal_error` |

Two things worth knowing as a client:

- **Requesting an update is idempotent.** While an update for a pair is
  pending, repeated requests return the identifier of that update instead of
  creating a new one.
- **Prices are JSON strings** (`"price":"20.3348"`), so that no precision is
  lost by parsing them as floating-point numbers.

## How it works

```mermaid
sequenceDiagram
    participant C as Client
    participant A as HTTP API
    participant D as PostgreSQL
    participant W as Worker
    participant P as Rate provider

    C->>A: POST update for EUR/MXN
    A->>D: insert a pending request, or get the pending one
    A-->>C: 202 Accepted with update_id

    loop every WORKER_INTERVAL
        W->>D: claim pending requests
        W->>P: fetch the rate
        P-->>W: rate
        W->>D: save the quote and complete the request
    end

    C->>A: GET update by id
    A->>D: read the request and its quote
    A-->>C: 200 OK with price and updated_at
```

1. The HTTP handler only stores a `pending` request and answers `202`. It
   never calls the rate provider.
2. The `quote_update_requests` table doubles as a job queue. The worker polls
   it, claims a batch of pending requests and processes them concurrently.
3. For each request the worker fetches the rate, then stores the quote and
   marks the request `completed` in a single transaction.
4. If the provider fails, the request stays `pending` and is retried later.
   After `WORKER_MAX_ATTEMPTS` attempts it becomes `failed`.

Several instances of the service can run against the same database: a request
is claimed with `FOR UPDATE SKIP LOCKED`, so no two workers process the same
one.

## Architecture

The service follows the hexagonal (ports and adapters) style. The business
logic sits in the middle and knows nothing about HTTP, PostgreSQL or the rate
provider; it only declares interfaces (ports) for what it needs. Adapters on
the outside implement those interfaces or call into the service.

```
cmd/server            wiring, start-up and graceful shutdown
internal/
  domain              currencies, pairs, quotes, update requests and their rules
  service             use cases and the ports they depend on
  handler             driving adapter: HTTP API
  worker              driving adapter: background processing loop
  repository          driven adapter: PostgreSQL
  provider            driven adapter: external rate provider
  system              driven adapter: wall clock and id generator
  config              configuration from environment variables
migrations            SQL migrations, embedded into the binary
api                   OpenAPI specification
```

Dependencies point inwards: adapters depend on `service` and `domain`,
`service` depends on `domain`, and `domain` depends on nothing in this
repository. `cmd/server` is the only place that knows every concrete type.

## Configuration

The service is configured with environment variables. Only `DATABASE_URL` is
required; Docker Compose sets it for you.

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | required | PostgreSQL connection string |
| `HTTP_ADDR` | `:8080` | Address the HTTP server listens on |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `SHUTDOWN_TIMEOUT` | `25s` | How long to wait for in-flight work on shutdown |
| `RATES_API_URL` | `https://api.frankfurter.dev/v1` | Base URL of the rate provider |
| `RATES_API_TIMEOUT` | `10s` | Timeout of one request to the provider |
| `WORKER_INTERVAL` | `1s` | How often the worker looks for pending updates |
| `WORKER_BATCH_SIZE` | `10` | How many updates are processed at once |
| `WORKER_MAX_ATTEMPTS` | `3` | Attempts before an update is failed |
| `WORKER_TIMEOUT` | `20s` | Deadline for processing one batch |
| `WORKER_STALE_AFTER` | `30s` | When a claimed update may be taken over by another worker |

Durations use Go syntax: `500ms`, `10s`, `1m30s`.

The timeouts depend on each other, and the service refuses to start if they
are inconsistent:

```
RATES_API_TIMEOUT < WORKER_TIMEOUT < WORKER_STALE_AFTER
                    WORKER_TIMEOUT < SHUTDOWN_TIMEOUT
```

A provider call must fit into a batch, a batch must end before its claim goes
stale, and a shutdown must leave a running batch time to finish.

## Development

Requirements: Go 1.27+, Docker (for PostgreSQL), and optionally
[golangci-lint](https://golangci-lint.run) v2.14.0.

### Run without Docker Compose

```bash
docker run -d --name quotes-pg -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17-alpine

export DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
go run ./cmd/server
```

### Tests

```bash
go test ./...
```

Without a database this runs the unit tests and skips the rest. To run
everything, point the tests at a PostgreSQL server:

```bash
export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
go test ./...
```

| Kind | Where | Needs |
|---|---|---|
| Unit | `domain`, `service`, `handler`, `worker`, `provider`, `config` | nothing |
| Integration | `repository` | PostgreSQL |
| End-to-end | `cmd/server` | PostgreSQL |
| Contract | `handler` | nothing |
| Live provider check | `provider` | network, `FRANKFURTER_LIVE=1` |

- Integration and end-to-end tests create a separate schema per test and drop
  it afterwards, so they run in parallel and leave the database clean.
- The contract tests check every handler response against
  [api/openapi.yaml](api/openapi.yaml), so the specification cannot drift
  away from the code unnoticed.
- The end-to-end tests start the whole service with a fake rate provider and
  drive it over HTTP.

CI runs the linter and `go test -race` with PostgreSQL on every pull request.

### Lint and format

```bash
golangci-lint run ./...
golangci-lint fmt
```

### Migrations

Migrations live in [migrations](migrations) in [goose](https://github.com/pressly/goose)
format. The service applies them on start-up under a PostgreSQL advisory lock,
so instances that start together do not migrate concurrently. To add a
currency, add a migration that inserts it into the `currencies` table.

## Design decisions

The most important decisions are below. The full list, with the alternatives
that were considered and the cost of each choice, is in
[docs/decisions.md](docs/decisions.md).

**The database is the job queue.** The update request and the job are the same
row, written by one `INSERT`, so a request can never be accepted without being
queued. Pending work survives restarts and is shared between instances. A
message broker would add infrastructure without adding guarantees at this
scale.

**Idempotency is enforced by the database.** A partial unique index allows one
pending request per pair. The request is stored with
`INSERT ... ON CONFLICT ... DO UPDATE ... RETURNING`, a single statement that
returns either the new row or the existing one. There is no check-then-insert
window for concurrent requests to slip through.

**A quote and an update request are separate things.** A quote is an immutable
fact: a price at a point in time. An update request is a process with a
lifecycle. They live in separate tables, and the request references the quote
it produced.

**A worker cannot overwrite the result of another worker.** Every claim
increments the attempt counter of the request, and the result is stored only
if the counter is unchanged since the claim. A worker that stalled and lost
its claim updates nothing and discards its result.

**A crashed worker does not lose requests.** A claim expires after
`WORKER_STALE_AFTER`, after which the request can be claimed again. The same
mechanism spaces out retries.

**Money is never a float.** Prices are `decimal.Decimal` in Go,
`NUMERIC(38, 18)` in PostgreSQL and strings in JSON. The provider response is
decoded straight into a decimal.

**Internal errors stay internal.** Clients get a fixed message for a `500`
and for a failed update; the details are logged and stored with the request.

**Graceful shutdown.** On `SIGTERM` the HTTP server finishes in-flight requests
and the worker finishes its current batch, so a deploy does not waste update
attempts.

**No ORM, no web framework.** SQL is written by hand because the queries that
matter are PostgreSQL-specific. Routing uses the standard library, which
supports method and path patterns since Go 1.22.

## Limitations

What could be done about each of these, and other possible improvements, is
described in [docs/roadmap.md](docs/roadmap.md).

- **Rates change once per working day.** The default provider,
  [Frankfurter](https://frankfurter.dev), publishes the reference rates of the
  European Central Bank. It was chosen because it needs no API key, so the
  project runs without registration. A real-time source can be added as
  another implementation of the `RateProvider` port.
- **Only ISO 4217 currencies are accepted.** The schema allows codes of up to
  10 characters, so supporting other assets requires relaxing one validation
  function and adding rows to the `currencies` table.
- **The requests table grows without bound.** A production deployment would
  delete or archive finished requests periodically.
- **Retries use a fixed delay** equal to `WORKER_STALE_AFTER`, not exponential
  backoff.
- **There is no authentication or rate limiting.** They are out of scope of
  the assignment.
