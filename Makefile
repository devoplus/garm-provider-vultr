SHELL := bash

ROOTDIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
GO ?= go

VERSION ?= $(shell git describe --tags --match='v[0-9]*' --dirty --always 2>/dev/null || echo "v0.0.0-unknown")
LDFLAGS := -s -w -X github.com/devoplus/garm-provider-vultr/provider.Version=$(VERSION) -X main.Version=$(VERSION)

default: build

.PHONY: build
build:
	$(GO) build -ldflags "$(LDFLAGS)" -o garm-provider-vultr .

# The race detector needs cgo. Run "make test RACE=" where no C toolchain is
# available; CI always runs with it enabled.
RACE ?= -race

.PHONY: test
test:
	$(GO) test -timeout=15m $(RACE) ./...

.PHONY: cover
cover:
	$(GO) test -timeout=15m -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: verify
verify: vet test

# The provider needs no cgo, so the release binaries cross compile without a
# toolchain container.
.PHONY: build-static
build-static:
	./scripts/build-static.sh

.PHONY: clean
clean:
	@rm -rf ./bin ./garm-provider-vultr ./garm-provider-vultr.exe coverage.out
