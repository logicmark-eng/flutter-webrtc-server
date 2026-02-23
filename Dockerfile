# --- Stage 1: Build ---
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/server/main.go

# --- Stage 2: Runtime ---
FROM alpine:3.19

RUN apk add --no-cache openssl

WORKDIR /app

COPY --from=builder /app/server ./server
COPY configs/ ./configs/
COPY web/     ./web/

# Generate a self-signed TLS certificate for HTTPS mode.
# The cert covers localhost and 127.0.0.1; replace with a real cert for production.
RUN mkdir -p configs/certs && \
    openssl req -x509 -newkey rsa:2048 \
      -keyout configs/certs/key.pem \
      -out  configs/certs/cert.pem \
      -days 3650 -nodes \
      -subj "/CN=localhost" \
      -addext "subjectAltName=IP:127.0.0.1,DNS:localhost" \
      2>/dev/null

EXPOSE 8086
EXPOSE 19302/udp
EXPOSE 19303/tcp

CMD ["./server"]
