.PHONY: build install test check check-integration check-all clean release

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X 'github.com/MuhametSmaili/hetki/internal/buildinfo.version=$(VERSION)' \
           -X 'github.com/MuhametSmaili/hetki/internal/buildinfo.commit=$(COMMIT)' \
           -X 'github.com/MuhametSmaili/hetki/internal/buildinfo.date=$(DATE)'

INTEGRATION_PACKAGES := ./internal/backend/tmux ./internal/app/start ./internal/app/tui ./internal/app/statusobserver

build:
	go build -ldflags "$(LDFLAGS)" -o hetki .

install: build
	sudo mv hetki /usr/local/bin/hetki

test:
	go test -v ./...

# CI uses the golangci-lint version in .golangci-lint-version; run the same or
# a newer v2.x locally.
check:
	golangci-lint fmt --diff
	go mod tidy -diff
	go mod verify
	go build ./...
	go test -shuffle=on ./...
	go vet ./...
	golangci-lint run --timeout=5m --build-tags=integration

# Require native tmux/PTY tools here, not in the ordinary check.
check-integration:
	@for tool in go tmux script stty python3; do \
		if ! command -v "$$tool" >/dev/null 2>&1; then \
			printf 'check-integration requires %s on PATH\n' "$$tool" >&2; exit 1; \
		fi; \
	done
	@tmux -V
	@for tool in node cc; do \
		if ! command -v "$$tool" >/dev/null 2>&1; then \
			printf 'Optional tool %s unavailable; related native fixtures may skip\n' "$$tool"; \
		fi; \
	done
	@if [ -n "$${HETKI_TEST_TMUX_LEGACY:-}" ] || [ -n "$${HETKI_TEST_TMUX_CURRENT:-}" ]; then \
		for binary in "$${HETKI_TEST_TMUX_LEGACY:-}" "$${HETKI_TEST_TMUX_CURRENT:-}"; do \
			if [ ! -f "$$binary" ] || [ ! -x "$$binary" ]; then \
				printf 'Set both HETKI_TEST_TMUX_LEGACY and HETKI_TEST_TMUX_CURRENT to executable paths\n' >&2; exit 1; \
			fi; \
			"$$binary" -V || exit 1; \
		done; \
	else \
		printf 'Mixed-version coverage: SKIP (set HETKI_TEST_TMUX_LEGACY and HETKI_TEST_TMUX_CURRENT to enable)\n'; \
	fi
	go vet -tags=integration $(INTEGRATION_PACKAGES)
	go test -race -count=1 -timeout=5m -tags=integration $(INTEGRATION_PACKAGES)

# Recursive recipes preserve order even when the caller uses make -j.
check-all:
	$(MAKE) check
	$(MAKE) check-integration

clean:
	rm -f hetki
	rm -rf dist/

# GoReleaser is the single release definition; this target also proves that
# two clean builds produce byte-identical artifacts.
release:
	./scripts/test-release-reproducibility.sh
