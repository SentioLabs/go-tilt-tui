# Every target CI runs. Install the tools first with `mise install`.
VERSION_PKG := github.com/sentiolabs/go-tilt-tui/internal/version
VERSION     ?= dev
GO_FILES    := $(shell git ls-files '*.go')

.PHONY: build test fmt-check tidy-check vet lint vuln pin-check shellcheck actionlint install-test check release-snapshot

# A plain build from a git checkout gets its commit and date from
# debug.ReadBuildInfo. VERSION=v0.1.0 sets the version string.
build:
	go build -trimpath -ldflags "-X $(VERSION_PKG).Version=$(VERSION)" -o bin/tilt-tui ./cmd/tilt-tui

test:
	go test -race ./...

fmt-check:
	@out="$$(gofmt -l $(GO_FILES))"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

tidy-check:
	go mod tidy -diff

vet:
	go vet ./...
	go vet -tags live ./...

lint:
	golangci-lint run ./...

vuln:
	scripts/vuln_scan.sh

pin-check:
	scripts/pin_check.sh

shellcheck:
	shellcheck scripts/*.sh

actionlint:
	actionlint

install-test:
	bash scripts/install_test.sh

check: fmt-check tidy-check vet test lint vuln pin-check shellcheck actionlint install-test

release-snapshot:
	goreleaser release --snapshot --clean
