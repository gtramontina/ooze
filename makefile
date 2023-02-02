PATH := $(PWD)/.bin:$(PATH)
SHELL := /usr/bin/env bash -eu -o pipefail
CPUS ?= $(shell (nproc --all || sysctl -n hw.ncpu) 2>/dev/null || echo 1)
MAKEFLAGS += --warn-undefined-variables --output-sync=line --jobs $(CPUS)

golangci-lint-version = v1.51.0
gotestsum-version = v1.9.0

.git/.hooks.log:
	@git config core.hooksPath .githooks
	@git config --get core.hooksPath > $@
pre-reqs += .git/.hooks.log

.bin/golangci-lint: $(pre-reqs)
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
	| sh -s -- -b $(PWD)/$(dir $@) -d $(golangci-lint-version)
pre-reqs += .bin/golangci-lint

.bin/gotestsum:
	@GOBIN="$(PWD)/$(dir $@)" go install gotest.tools/gotestsum@$(gotestsum-version)
pre-reqs += .bin/gotestsum

test: $(pre-reqs)
	@gotestsum --format-hide-empty-pkg -- -race -cover -timeout=60s -shuffle=on ./...
.PHONY: test

test.failfast: $(pre-reqs)
	@gotestsum --format-hide-empty-pkg --max-fails=1 -- -timeout=60s -failfast ./...
.PHONY: test.failfast

test.mutation: $(pre-reqs)
	@go test -timeout=30m -count=1 -v -tags=mutation
.PHONY: test.mutation

lint: $(pre-reqs)
	@golangci-lint -v run
.PHONY: lint
