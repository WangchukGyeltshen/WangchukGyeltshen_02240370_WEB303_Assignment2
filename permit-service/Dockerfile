# Multi-stage build for the Permit Service (Service A).
# Build context is the repo root so the shared go.mod and proto/ are available.

# ---------- Stage 1: build the Go binary ----------
FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Permit Service needs both protos (its own, plus Route's for the client).
COPY proto/ ./proto/
COPY permit-service/ ./permit-service/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/permit-service ./permit-service

# ---------- Stage 2: minimal runtime image ----------
FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=builder /out/permit-service /app/permit-service
USER appuser
ENTRYPOINT ["/app/permit-service"]