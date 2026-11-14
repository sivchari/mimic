.DEFAULT_GOAL := help

.PHONY: help
help: ## Show the available targets.
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-16s %s\n", $$1, $$2}'

.PHONY: test
test: ## Run all tests (unit, golden and end-to-end).
	go test ./... -shuffle on -race

.PHONY: test-coverage
test-coverage: ## Run tests with a coverage report across all packages.
	go test -race -coverprofile=coverage.out -covermode=atomic -coverpkg=./... ./...
	go tool cover -func=coverage.out

.PHONY: update-golden
update-golden: ## Regenerate the golden files in testdata/golden.
	go test ./cmd/mimic -run TestGolden -update

.PHONY: lint
lint: ## Run go vet analyzers and modernize through gokui.
	gokui lint

.PHONY: fmt
fmt: ## Format the code with gokui (gofmt and goimports).
	gokui fmt

.PHONY: build
build: ## Build the mimic binary into ./bin.
	go build -o bin/mimic ./cmd/mimic
