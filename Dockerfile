# Multi-stage build for REMITT server
# -----------------------------------
# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-s -w" \
    -o /remitt-server ./cmd/remitt-server/

# Stage 2: Minimal runtime
FROM debian:stable-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    xsltproc \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /remitt-server /usr/local/bin/remitt-server

# Default config; mount your own at runtime
COPY remitt.yml /etc/remitt/remitt.yml

# Static UI
COPY ui/ /opt/remitt/ui/

# Migrations
COPY migrations/ /opt/remitt/migrations/

# XSLT / validation scripts
COPY resources/ /opt/remitt/resources/

WORKDIR /opt/remitt

EXPOSE 3000

ENTRYPOINT ["/usr/local/bin/remitt-server"]
CMD ["--config-file", "/etc/remitt/remitt.yml"]
