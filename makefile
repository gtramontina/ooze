PATH := $(PWD)/.bin:$(PATH)
SHELL := /usr/bin/env bash -eu -o pipefail
CPUS ?= $(shell (nproc --all || sysctl -n hw.ncpu) 2>/dev/null || echo 1)
MAKEFLAGS += --warn-undefined-variables --output-sync=line --jobs $(CPUS)

.git/.hooks.log:
	@git config core.hooksPath .githooks
	@git config --get core.hooksPath > $@
pre-reqs += .git/.hooks.log

test: $(pre-reqs)
	@gotestsum --format-hide-empty-pkg -- -race -cover -timeout=60s -shuffle=on ./...
.PHONY: test

test.failfast: $(pre-reqs)
	@gotestsum --format-hide-empty-pkg --max-fails=1 -- -timeout=60s -failfast ./...
.PHONY: test.failfast

test.mutation: $(pre-reqs)
	@go test -timeout=30m -count=1 -v -tags=mutation
.PHONY: test.mutation

# Adversarial process fixtures. Held out of `test` because they reproduce
# containment failures that are not all fixed yet; see the fixture comments for
# which platforms contain each behaviour.
test.adversarial: $(pre-reqs)
	@gotestsum --format=testname -- -count=1 -timeout=120s -tags=adversarial ./internal/cmdtestrunner/...
.PHONY: test.adversarial

lint: $(pre-reqs)
	@golangci-lint -v run
.PHONY: lint
