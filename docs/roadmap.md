# Possible improvements

What the service does not do yet and how it could grow. Nothing here is
required by the assignment; the list shows where the current design stops and
what each next step would take.

The items are grouped by purpose, not ordered by priority. If the service
were going to production, the operations group would come first.

## Operations

| Improvement | What it gives | What it takes |
|---|---|---|
| Readiness check | `/healthz` reports only that the process is alive. A separate `/readyz` that pings the database would let an orchestrator stop routing traffic to an instance that cannot work. | One handler and a `Ping` method on the storage port. |
| Metrics | Queue length, age of the oldest pending request, share of failed updates, provider latency. Today a growing backlog is invisible. | A metrics endpoint and counters in the worker and the provider adapter. |
| Request logging and request ids | One log record per HTTP request, and an id that ties together the records of one request. | An HTTP middleware; the id travels in the context. |
| Separate migration step | Migrations applied by the deployment pipeline instead of by the service on start-up, so that a bad migration does not take the service down. | A second entry point or a flag that runs migrations and exits. |
| Container health check | Docker cannot probe the service: the image has no shell. | A `-healthcheck` flag that calls `/healthz` and exits with its result. |

## Reliability

| Improvement | What it gives | What it takes |
|---|---|---|
| Clean-up of finished requests | `quote_update_requests` grows without bound. | A periodic job that deletes or archives requests older than a retention period, or partitioning by time. |
| Exponential backoff | Retries currently happen at a fixed delay, `WORKER_STALE_AFTER`. Backoff would ease the load on a struggling provider. | A "not before" column and a change to the claim condition. |
| Circuit breaker for the provider | When the provider is down, every request still spends all its attempts. A breaker would pause processing instead. | State in the provider adapter; requests would stay pending without losing attempts. |
| Wake-up signal | Processing starts up to `WORKER_INTERVAL` after a request. | PostgreSQL `LISTEN/NOTIFY` on top of polling; polling stays as the fallback. |

## Functionality

| Improvement | What it gives | What it takes |
|---|---|---|
| Real-time rate source | The default provider publishes rates once per working day. | Another implementation of the `RateProvider` port. Nothing else changes. |
| Fallback provider | A second source used when the first one fails. | A `RateProvider` that wraps several others. |
| `Idempotency-Key` header | A retry returns the same update even after it is finished. Today only pending updates are deduplicated. | A nullable column with a unique index, and a retention period for keys. |
| More currencies | Any ISO 4217 currency the provider knows. | A migration that inserts rows into `currencies`. |
| Non-ISO assets | Crypto tickers such as `USDT`. | Relaxing `NewCurrency`; the schema already allows codes of up to 10 characters. |
| Quote history | The `quotes` table already keeps every price obtained. | A read endpoint with a time range and pagination. |
| Scheduled updates | Quotes refreshed periodically without a client asking. | A scheduler that calls `RequestUpdate`. Quotes do not depend on how they were requested, so nothing else changes. |

## API

| Improvement | What it gives | What it takes |
|---|---|---|
| Uniform errors from the router | `404` and `405` for unknown paths and methods are plain text, unlike every other error. | A wrapper around the router that rewrites them. |
| Authentication and rate limiting | The API is open and unlimited. | Usually a gateway in front of the service rather than code in it. |
| Stricter response schemas | The contract tests accept fields that the specification does not describe. | `additionalProperties: false` in the response schemas, at the cost of making every new field a breaking change for strict clients. |
| Request validation in contract tests | Only responses are checked against the specification today. | Validating requests too, except in the tests that send malformed input on purpose. |
