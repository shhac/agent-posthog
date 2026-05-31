BINARY := agent-posthog
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod
GOENV := GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE)

build:
	$(GOENV) go build -buildvcs=false -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/agent-posthog

build-mock:
	$(GOENV) go build -buildvcs=false -o mockposthog ./cmd/mockposthog

mock:
	$(GOENV) go run ./cmd/mockposthog

mock-dev:
	AGENT_POSTHOG_BASE_URL=http://127.0.0.1:18118 POSTHOG_PERSONAL_API_KEY=phx_mock $(GOENV) go run ./cmd/agent-posthog $(ARGS)

test:
	$(GOENV) go test ./... -count=1

test-short:
	$(GOENV) go test ./... -count=1 -short

lint:
	GOLANGCI_LINT_CACHE=$(CURDIR)/.cache/golangci-lint $(GOENV) golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

clean:
	rm -f $(BINARY)
	rm -f mockposthog
	rm -rf dist/

dev:
	$(GOENV) go run ./cmd/agent-posthog $(ARGS)

vet:
	$(GOENV) go vet ./...

.PHONY: build build-mock mock mock-dev test test-short lint fmt clean dev vet
