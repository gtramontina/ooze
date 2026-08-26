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
	@go test -timeout=90m -count=1 -v -tags=mutation -run=^TestMutation$
.PHONY: test.mutation

test.adversarial: $(pre-reqs)
	@gotestsum --format=testname -- -count=1 -timeout=120s -tags=adversarial ./internal/ooze/...
.PHONY: test.adversarial

test.crosscompile: $(pre-reqs)
	@for target in linux/amd64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 plan9/amd64; do \
		GOOS="$${target%/*}" GOARCH="$${target#*/}" CGO_ENABLED=0 \
			go test -exec=true ./internal/ooze; \
	done
.PHONY: test.crosscompile

lint:
	@golangci-lint run --no-config ./...
.PHONY: lint
