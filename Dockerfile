# syntax=docker/dockerfile:1

# Build stage: compiles a static binary. Nothing from this stage except the
# binary ends up in the final image.
FROM golang:1.27-alpine AS build

WORKDIR /src

# Dependencies are downloaded in their own layer, so that it is rebuilt only
# when go.mod or go.sum change, not on every source change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is off to get a binary with no dependency on system libraries; it can
# then run in an image that has none. -trimpath and -s -w drop local paths
# and debug symbols.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Runtime stage: a minimal image with CA certificates (needed to call the
# rate provider over HTTPS) and time zone data, but no shell and no package
# manager.
FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/server /server

# The service does not need root; the image provides an unprivileged user.
USER nonroot:nonroot

EXPOSE 8080

# Exec form: the binary is PID 1 and receives SIGTERM directly, which is
# what triggers the graceful shutdown.
ENTRYPOINT ["/server"]
