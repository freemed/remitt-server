# Multi-stage build for REMITT server (single-directory context)
#   docker build -t remitt-server .        # from the remitt-server repo root

# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Full source first: the root go.mod has local `replace => ./api` etc. directives,
# so every workspace module must already exist before `go mod download`.
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-s -w" \
    -o /remitt-server ./cmd/remitt-server/

# Stage 2: Minimal runtime
FROM alpine:latest

RUN apk add --no-cache \
    ca-certificates \
    libxslt

# libxslt (and therefore xsltproc) is retained as the external XSLT fallback:
# the built-in in-process engine is the default and matches xsltproc
# byte-for-byte on the shipped stylesheets, but a deployment can switch back
# with `internal-xslt: false`, and common.XslTransform falls back to this binary
# automatically if the in-process engine errors.

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
