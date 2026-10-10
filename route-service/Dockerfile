# Multi-stage build for the Route Service (Service B).
# Build context is the repo root so the shared go.mod and proto/ are available.

# ---------- Stage 1: build the Go binary ----------
FROM golang:1.27-alpine AS builder
WORKDIR /src

# Download dependencies first (cached unless go.mod/go.sum change).
COPY go.mod go.sum ./
RUN go mod download

# Copy only the code this service needs.
COPY proto/ ./proto/
COPY route-service/ ./route-service/

# Static binary, stripped of debug info for a smaller image.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/route-service ./route-service

# ---------- Stage 2: minimal runtime image ----------
FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=builder /out/route-service /app/route-service
USER appuser
ENTRYPOINT ["/app/route-service"]