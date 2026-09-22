BINARY   := team-assigner
BINDIR   := bin
SEED     ?= 1
MAX_SIZE ?= 4
MIN_SIZE ?= $(shell expr $(MAX_SIZE) - 1)

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: ## Build the binary into ./bin/
	go build -o $(BINDIR)/$(BINARY) ./cmd/team-assigner

test: ## Run the full test suite
	go test ./...

cover: ## Run the test suite with a coverage summary
	go test -cover ./...

vet: ## Run go vet over all packages
	go vet ./...

fmt: ## Fail if any file needs gofmt
	@test -z "$$(gofmt -l .)" || (echo "needs gofmt:"; gofmt -l .; exit 1)

run-example: build ## Assign the synthetic sample data into ./bin/teams.csv
	./$(BINDIR)/$(BINARY) assign \
		-input testdata/sample_votes.csv \
		-output $(BINDIR)/teams.csv \
		-seed $(SEED) \
		-max-size $(MAX_SIZE) \
		-min-size $(MIN_SIZE)

clean: ## Remove build artifacts
	rm -rf $(BINDIR)

.PHONY: help build test cover vet fmt run-example clean
