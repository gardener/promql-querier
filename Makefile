# SPDX-FileCopyrightText: Contributors to the Gardener project
# SPDX-License-Identifier: Apache-2.0

PROMQL_QUERIER_BINARY := bin/promql-querier

GOLANGCI_LINT := $(shell command -v golangci-lint 2>/dev/null || echo $(shell go env GOPATH)/bin/golangci-lint)

.PHONY: build
build:
	go build -o $(PROMQL_QUERIER_BINARY) ./cmd/promql-querier

.PHONY: ui
ui:
	@echo "> Build UI bundle"
	rm -rf ui/dist/assets ui/dist/app.html
	mkdir -p ui/src/assets
	cp logo/gardener.svg ui/src/assets/gardener.svg
	cd ui && npm ci && npm run build
	@echo "> Generate third-party license attribution for the embedded UI bundle"
	cd ui && npx --yes generate-license-file@4.2.5 --input package.json --output dist/assets/third-party-licenses.txt --overwrite

.PHONY: format
format: $(GOLANGCI_LINT)
	@echo "> Format"
	@$(GOLANGCI_LINT) fmt
	@echo "> UI format"
	cd ui && npm ci && npm run fmt

.PHONY: vet
vet:
	@echo "> Vet"
	go vet -tags compare ./...

.PHONY: lint
lint: $(GOLANGCI_LINT)
	@echo "> Lint"
	$(GOLANGCI_LINT) run ./...

.PHONY: check
check: vet lint
	@echo "> UI check"
	cd ui && npm ci && npm run check

$(GOLANGCI_LINT):
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

.PHONY: test
test:
	go test -race -v -timeout 300s ./...
	cd ui && npm ci && npm test

.PHONY: test-compare
test-compare:
	go test -count 1 -race -v -timeout 300s -tags compare ./test/compare/tests/

.PHONY: cover
cover:
	@mkdir -p tmp
	go test -race -timeout 300s -coverprofile=tmp/coverage.out ./...
	go tool cover -func=tmp/coverage.out
	go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "> HTML report written to tmp/coverage.html"

.PHONY: prometheus
prometheus:
	@scripts/download-prometheus.sh

.PHONY: playground
playground:
	go run ./internal/playground
