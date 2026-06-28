BINARY := testgen-loop
PKG    := ./cmd/testgen-loop
OUT    := bin/$(BINARY)

GO        ?= go
GOFLAGS   ?=
TEST_PKGS ?= ./...

.PHONY: all build run test vet fmt tidy clean help

all: build

## build: compile the binary into ./bin/
build:
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o $(OUT) $(PKG)

## run: build and run; pass extra args via ARGS, e.g. make run ARGS="--help"
run: build
	./$(OUT) $(ARGS)

## test: run unit tests with race detector
test:
	$(GO) test -race $(GOFLAGS) $(TEST_PKGS)

## vet: run go vet across the module
vet:
	$(GO) vet $(TEST_PKGS)

## fmt: format all Go source in place
fmt:
	$(GO) fmt $(TEST_PKGS)

## tidy: sync go.mod and go.sum
tidy:
	$(GO) mod tidy

## clean: remove build output and coverage artifacts
clean:
	rm -rf bin coverage.out coverage.html

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
