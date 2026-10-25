# REMITT server — build, test and run
#
# Layout note: this is a Go *workspace* (go.work) with a root module plus ten
# sub-modules. `go build ./...` from the root only sees root-module packages,
# so the test targets walk the module list explicitly. That list is derived
# from go.work and can be overridden on the command line.
#
# `make check` mirrors .github/workflows/go.yml exactly: `go build ./...`, then
# `go test ./...` plus a test pass over every sub-module. If CI is red, `make
# check` is red.

SHELL     := /bin/bash
GO        ?= go
GOFMT     ?= gofmt
GOFLAGS   ?=
TESTFLAGS ?=
LDFLAGS   ?=

BIN     ?= bin
CMD_DIRS := $(notdir $(wildcard cmd/*))
BINS     := $(addprefix $(BIN)/,$(CMD_DIRS))

# Sub-modules in the order CI tests them. ".", the root module, is excluded:
# `test-root` covers it.
MODULES ?= $(shell sed -n '/^use (/,/^)/p' go.work \
             | grep -v '^use (\|^)' \
             | awk 'NF{print $$1}' \
             | grep -v '^\.$$')

CONFIG      ?= ./remitt.yml
ARGS        ?=
COVER       ?= coverage.out
IMAGE       ?= remitt-server
TAG         ?= latest
PORT        ?= 3000
LIVE_CONFIG ?= ./remitt.yml

# Binaries the suite looks for. A missing optional tool turns tests into SKIPs,
# which read like passes in a green summary — see `check-tools`.
REQUIRED_TOOLS := go make
OPTIONAL_TOOLS := golangci-lint xsltproc docker mysql ssh-keygen

.DEFAULT_GOAL := build
.PHONY: help build release run run-harness test test-root test-modules test-race \
        test-live cover cover-html fmt fmt-check vet lint check check-tools tidy \
        docker-build docker-run clean

help: ## Show this help
	@echo "REMITT server"
	@echo ""
	@echo "Usage: make <target> [VAR=value]"
	@echo ""
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Common variables:"
	@echo "  CONFIG=$(CONFIG)   config file for run / test-live"
	@echo "  ARGS='-debug'             extra flags for run / run-harness"
	@echo "  TESTFLAGS='-v -run X'     extra flags for the test targets"
	@echo "  BIN=$(BIN)                binary output directory"
	@echo ""
	@echo "Sub-modules tested by 'make test' (read from go.work):"
	@echo "  $(MODULES)"

## ---- build ----------------------------------------------------------------

build: ## Build every cmd/* binary (always rebuilds)
	@mkdir -p $(BIN)
	@set -e; for d in $(CMD_DIRS); do \
	  echo "==> build cmd/$$d -> $(BIN)/$$d"; \
	  $(GO) build $(GOFLAGS) $(if $(LDFLAGS),-ldflags "$(LDFLAGS)") -o $(BIN)/$$d ./cmd/$$d; \
	done
	@echo "built: $(BINS)"

release: GOFLAGS  += -trimpath
release: LDFLAGS  := -s -w
release: build ## Build stripped, -trimpath release binaries

## ---- run ------------------------------------------------------------------

run: ## Run the server (make run ARGS='-debug')
	$(GO) run ./cmd/remitt-server/ -config-file $(CONFIG) $(ARGS)

run-harness: ## Run the standalone SFTP test harness (prints connection details)
	$(GO) run ./cmd/sshsftp-harness/ $(ARGS)

## ---- test -----------------------------------------------------------------

test: test-root test-modules ## Run all tests: root module + every sub-module (CI)

test-root: ## Test the root module
	$(GO) test -count=1 $(TESTFLAGS) ./...

test-modules: ## Test every go.work sub-module
	@set -e; for m in $(MODULES); do \
	  echo "== $$m =="; \
	  (cd $$m && $(GO) test -count=1 $(TESTFLAGS) ./...); \
	done

test-race: ## Run all tests with the race detector
	$(GO) test -race -count=1 $(TESTFLAGS) ./...
	@set -e; for m in $(MODULES); do \
	  echo "== race: $$m =="; \
	  (cd $$m && $(GO) test -race -count=1 $(TESTFLAGS) ./...); \
	done

test-live: ## Run the suite against a live MySQL (sets REMITT_LIVE_CONFIG; needs real credentials)
	REMITT_LIVE_CONFIG=$(abspath $(LIVE_CONFIG)) $(GO) test -count=1 $(TESTFLAGS) ./...
	@set -e; for m in $(MODULES); do \
	  echo "== live: $$m =="; \
	  (cd $$m && REMITT_LIVE_CONFIG=$(abspath $(LIVE_CONFIG)) $(GO) test -count=1 $(TESTFLAGS) ./...); \
	done

cover: ## Root-module tests with coverage (COVER=coverage.out)
	$(GO) test -count=1 -coverprofile=$(COVER) ./...
	$(GO) tool cover -func=$(COVER) | tail -1

cover-html: cover ## Render coverage to coverage.html
	$(GO) tool cover -html=$(COVER) -o coverage.html
	@echo "wrote coverage.html"

## ---- quality gates --------------------------------------------------------

fmt: ## Format every module in place
	@set -e; for d in . $(MODULES); do \
	  (cd $$d && $(GO) list -f '{{.Dir}}' ./... | xargs -r $(GOFMT) -w); \
	done

fmt-check: ## Fail if any module is not gofmt-clean
	@out=$$(set -e; for d in . $(MODULES); do \
	  (cd $$d && $(GO) list -f '{{.Dir}}' ./... | xargs -r $(GOFMT) -l); \
	done); \
	if [ -n "$$out" ]; then \
	  echo "not gofmt-clean (run 'make fmt'):"; echo "$$out"; exit 1; \
	fi; echo "gofmt: clean"

vet: ## Run go vet on every module
	$(GO) vet ./...
	@set -e; for m in $(MODULES); do (cd $$m && $(GO) vet ./...); done

lint: ## Run golangci-lint on every module (requires golangci-lint)
	@command -v golangci-lint >/dev/null 2>&1 || { \
	  echo "golangci-lint not found on PATH"; \
	  echo "install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	  exit 1; }
	golangci-lint run ./...
	@set -e; for m in $(MODULES); do \
	  echo "== lint: $$m =="; \
	  (cd $$m && golangci-lint run ./...); \
	done

check: build test ## CI gate: mirrors .github/workflows/go.yml (build + all tests)
	@echo "check: OK"

check-tools: ## Report external tools the build and tests need
	@echo "required:"
	@for t in $(REQUIRED_TOOLS); do \
	  if command -v $$t >/dev/null 2>&1; then \
	    printf "  [ ok ] %-14s %s\n" "$$t" "$$(command -v $$t)"; \
	  else \
	    printf "  [MISS] %-14s required - install it\n" "$$t"; \
	  fi; \
	done
	@echo "optional (missing => tests skip, and a skip reads like a pass):"
	@for t in $(OPTIONAL_TOOLS); do \
	  if command -v $$t >/dev/null 2>&1; then \
	    printf "  [ ok ] %-14s %s\n" "$$t" "$$(command -v $$t)"; \
	  else \
	    printf "  [ -- ] %-14s not installed\n" "$$t"; \
	  fi; \
	done
	@echo ""
	@echo "live tests also need a reachable MySQL: make test-live LIVE_CONFIG=$(LIVE_CONFIG)"

tidy: ## go mod tidy in the root module
	$(GO) mod tidy

## ---- docker ---------------------------------------------------------------

docker-build: ## Build the container image (IMAGE:TAG)
	docker build -t $(IMAGE):$(TAG) .

docker-run: ## Run the image, mounting CONFIG as the server config
	docker run --rm -p $(PORT):3000 -v $(abspath $(CONFIG)):/etc/remitt/remitt.yml $(IMAGE):$(TAG)

## ---- housekeeping ---------------------------------------------------------

clean: ## Remove binaries, coverage output and test artifacts
	rm -rf $(BIN) coverage.out coverage.html
	rm -f test/*.pdf test/*.x12 test/*.txt
	@echo "clean"
