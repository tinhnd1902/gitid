SHELL := /bin/bash
GO ?= go
VERSION ?= dev
LDFLAGS := -X main.version=$(VERSION)

# Core packages and their minimum statement coverage (percent). See plan N5.
CORE85 := remote gitcfg sshcfg block fsx plan config
CORE70 := app keys gitops doctor importer

.PHONY: build test cover vet fmt-check lint test-integration check-home install clean

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/gitid ./cmd/gitid

test:
	$(GO) test -race ./...

# cover: run tests with coverage, print per-package results and enforce thresholds.
cover:
	@$(GO) test -cover ./internal/... | tee coverage.out
	@fail=0; \
	for p in $(CORE85); do \
	  pct=$$(grep -E "/internal/$$p[[:space:]]" coverage.out | sed -E 's/.*coverage: ([0-9.]+)%.*/\1/'); \
	  if [ -z "$$pct" ]; then echo "no coverage for $$p"; fail=1; continue; fi; \
	  if [ $$(echo "$$pct < 85" | bc -l) -eq 1 ]; then echo "FAIL $$p $$pct% < 85%"; fail=1; fi; \
	done; \
	for p in $(CORE70); do \
	  pct=$$(grep -E "/internal/$$p[[:space:]]" coverage.out | sed -E 's/.*coverage: ([0-9.]+)%.*/\1/'); \
	  if [ -z "$$pct" ]; then echo "no coverage for $$p"; fail=1; continue; fi; \
	  if [ $$(echo "$$pct < 70" | bc -l) -eq 1 ]; then echo "FAIL $$p $$pct% < 70%"; fail=1; fi; \
	done; \
	exit $$fail

vet:
	$(GO) vet ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

# lint: golangci-lint when installed, else vet + fmt-check.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; \
	else echo "golangci-lint not found: falling back to vet + fmt-check"; $(MAKE) --no-print-directory vet fmt-check; fi

test-integration:
	$(GO) test -tags integration ./...

# check-home: only cmd/ and internal/env/fromos.go may read HOME / XDG from the process.
check-home:
	@bad=$$(grep -rnE 'os\.UserHomeDir|os\.Getenv\("(HOME|XDG_CONFIG_HOME)"\)|os\.LookupEnv\("(HOME|XDG_CONFIG_HOME)"\)' --include='*.go' . | grep -vE '^\./(cmd/|internal/env/fromos\.go)' || true); \
	if [ -n "$$bad" ]; then echo "check-home FAILED: direct HOME/XDG access outside allowed files:"; echo "$$bad"; exit 1; fi; \
	echo "check-home OK"

install:
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/gitid

clean:
	rm -rf bin dist coverage.out

# release-snapshot: local GoReleaser dry run (no publishing). The target only
# exists when goreleaser is installed.
ifneq ($(shell command -v goreleaser 2>/dev/null),)
.PHONY: release-snapshot
release-snapshot:
	goreleaser release --snapshot --clean
endif
