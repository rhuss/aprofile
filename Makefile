# aprofile build and check targets.
GO ?= go
BINDIR ?= bin

.PHONY: all build test vet fmt fmt-check tidy clean check

all: check build

build:
	$(GO) build -o $(BINDIR)/aprofile ./cmd/aprofile

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

tidy:
	$(GO) mod tidy

# The gate every change must pass before merge (constitution Development Workflow).
check: fmt-check vet test

clean:
	rm -rf $(BINDIR) dist
