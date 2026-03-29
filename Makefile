# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GO=go
GO_MAJOR_VERSION = $(shell $(GO) version | cut -c 14- | cut -d' ' -f1 | cut -d'.' -f1)
GO_MINOR_VERSION = $(shell $(GO) version | cut -c 14- | cut -d' ' -f1 | cut -d'.' -f2)

GO_VERSION = $(GO_MAJOR_VERSION).$(GO_MINOR_VERSION)

PACKAGE_NAME = $(shell awk '/^module / {print $$2}' go.mod)

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: alfred
alfred: ## Make Alfred for Go Bindings
	make -C internal/alfred/alfred

.PHONY: frontend
frontend: ## Build the React frontend (outputs to static/).
	cd frontend && npm install && npx vite build

.PHONY: sqlc-gen
sqlc-gen: ## Generate sqlc code
	$(GOBIN)/sqlc generate

.PHONY: build
build: fmt vet buf sqlc-gen frontend whisper-embed ## Build manager binary.
	GOCACHE=$(pwd)/.gocache CGO_ENABLED=1 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/openmanetd main.go

.PHONY: run
run: fmt vet buf sqlc-gen ## Run a controller from your host.
	go run ./main.go

.PHONY: buf
buf: ## Generate protobuf code
	buf format -w proto
	buf generate

.PHONY: test
test: fmt vet buf sqlc-gen ## Run tests.
	go test ./... -coverprofile=coverage.out -covermode=atomic

.PHONY: test-race
test-race: fmt vet buf sqlc-gen ## Run tests with race detector.
	go test -race -timeout 120s ./... -coverprofile=coverage.out -covermode=atomic

.PHONY: integration-test
integration-test: fmt vet ## Run integration tests (no hardware required).
	go test -tags integration -timeout 60s ./internal/openmanet/server/handlers/... -coverprofile=coverage.out -covermode=atomic

.PHONY: test-frontend
test-frontend: ## Run frontend tests.
	cd frontend && npm install && npx vitest run

.PHONY: lint
lint: ## Run linters.
	$(GOBIN)/golangci-lint run --timeout 5m

.PHONY: bench-comms
bench-comms: ## Run performance benchmarks on the comms package.
	go test ./internal/comms/ -bench=. -benchmem -count=3 -run=^$$ -timeout 120s

.PHONY: fuzz
fuzz: ## Run fuzz tests for 30 seconds each.
	go test ./internal/security/... -fuzz=Fuzz -fuzztime=30s -run=^$$
	go test ./internal/comms/... -fuzz=Fuzz -fuzztime=30s -run=^$$

.PHONY: build-lite
build-lite: fmt vet frontend ## Build lite binary without whisper, UPX compressed (~5MB).
	@rm -rf static/whisper
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/openmanetd-webui .
	@if command -v upx >/dev/null 2>&1; then \
		upx --lzma --best bin/openmanetd-webui; \
	else \
		echo "WARNING: upx not found, skipping compression (install with: apt install upx-ucl)"; \
	fi
	@echo "Built bin/openmanetd-webui (lite, no whisper, UPX compressed)"

.PHONY: whisper-embed
whisper-embed: ## Copy whisper WASM + model into static/ for embedding.
	@mkdir -p static/whisper
	@if [ ! -f whisper/whisper-main.js ]; then \
		echo "ERROR: whisper/whisper-main.js not found."; \
		echo "Download it from https://whisper.ggerganov.com/ or build from whisper.cpp"; \
		exit 1; \
	fi
	@if [ ! -f whisper/ggml-tiny.en.bin ]; then \
		echo "Downloading whisper tiny.en model (75MB)..."; \
		curl -fSL -o whisper/ggml-tiny.en.bin \
			"https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin"; \
	fi
	cp whisper/whisper-main.js static/whisper/
	cp whisper/ggml-tiny.en.bin static/whisper/
	@echo "Whisper files staged in static/whisper/ (will be embedded in binary)"

.PHONY: whisper-clean
whisper-clean: ## Remove whisper files from static/ (before lite build).
	rm -rf static/whisper

.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf bin/ static/whisper

