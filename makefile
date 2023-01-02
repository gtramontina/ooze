PATH := $(PWD)/.bin:$(PATH)
SHELL := /usr/bin/env bash -eu -o pipefail
CPUS ?= $(shell (nproc --all || sysctl -n hw.ncpu) 2>/dev/null || echo 1)
MAKEFLAGS += --warn-undefined-variables --output-sync=line --jobs $(CPUS)

golangci-lint-version = v1.50.1

.git/.hooks.log:
	@git config core.hooksPath .githooks
	@git config --get core.hooksPath > $@
pre-reqs += .git/.hooks.log

.bin/golangci-lint: $(pre-reqs)
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
	| sh -s -- -b $(PWD)/.bin -d $(golangci-lint-version)
pre-reqs += .bin/golangci-lint

test: $(pre-reqs)
	@go test -v -race -cover -shuffle=on ./...
.PHONY: test

test.mutation: $(pre-reqs)
	@go test -count=1 -tags=mutation
.PHONY: test.mutation

lint: $(pre-reqs)
	@golangci-lint -v run
.PHONY: lint
