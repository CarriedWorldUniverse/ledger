VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/CarriedWorldUniverse/ledger/internal/version.Version=$(VERSION)

.PHONY: build test vet version clean

build:
	@echo "ledger has no standalone binaries yet (Phase 0 scaffold); imported as a Go module by nexus.exe"
	go build ./...

test:
	go test -race ./...

vet:
	go vet ./...

version:
	@echo $(VERSION)

clean:
	rm -rf bin/
