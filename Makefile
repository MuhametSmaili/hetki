.PHONY: build install test check clean release

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X 'github.com/MuhametSmaili/hetki/cmd.Version=$(VERSION)' \
           -X 'github.com/MuhametSmaili/hetki/cmd.GitCommit=$(COMMIT)' \
           -X 'github.com/MuhametSmaili/hetki/cmd.BuildDate=$(DATE)'

build:
	go build -ldflags "$(LDFLAGS)" -o hetki .

install: build
	sudo mv hetki /usr/local/bin/hetki

test:
	go test -v ./...

# Requires golangci-lint v2.14.0 on PATH, matching CI.
check:
	@unformatted="$$(gofmt -l .)" || exit $$?; \
	if [ -n "$$unformatted" ]; then \
		printf 'Go files need formatting:\n%s\n' "$$unformatted" >&2; \
		exit 1; \
	fi
	go test ./...
	go vet ./...
	golangci-lint run --timeout=5m --build-tags=integration

clean:
	rm -f hetki
	rm -rf dist/

# GoReleaser is the single release definition; this target also proves that
# two clean builds produce byte-identical artifacts.
release:
	./scripts/test-release-reproducibility.sh
