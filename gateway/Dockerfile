# Multi-stage build for the API Gateway.
# Build context is the repo root so the shared go.mod and proto/ are available.

# ---------- Stage 1: build the Go binary ----------
FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto/ ./proto/
COPY gateway/ ./gateway/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/gateway ./gateway

# ---------- Stage 2: minimal runtime image ----------
FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=builder /out/gateway /app/gateway
USER appuser
ENTRYPOINT ["/app/gateway"]