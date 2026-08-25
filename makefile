PATH := $(PWD)/.bin:$(PATH)
SHELL := /usr/bin/env bash -eu -o pipefail
CPUS ?= $(shell (nproc --all || sysctl -n hw.ncpu) 2>/dev/null || echo 1)
STRESS_COUNT ?= 10
ACCEPTANCE_TESTS := ^(TestSupervisedDomainPlatformContract|TestSupervisedDomainDrainsAWideFanout|TestFixtureTeardownTreatsAnUnreapedDescendantAsDrained|TestNativeSupervisorDrainExpiryNeverManufacturesEmptiness|TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember|TestDarwinCensusInstrumentsPerDescendantShape|TestLinuxNativeSupervisorReapsOrphanedEscapeeThroughGuardian|TestLinuxSubreaperVisibilityPerDescendantShapeAndRootState|TestWindowsNativeSupervisorRejectsBreakawayFromJob|TestWindowsNativeSupervisorDrainsChildInNestedJob|TestWindowsNativeJobKillOnCloseStopsExactSubject|TestWindowsJobVisibilityPerDescendantShapeAndRootState)$$
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

# Adversarial process fixtures, including explicit platform-limit assertions.
test.adversarial: $(pre-reqs)
	@gotestsum --format=testname -- -count=1 -timeout=120s -tags=adversarial ./internal/ooze/...
.PHONY: test.adversarial

# One bounded pull-request acceptance pass through every native contract seam.
test.acceptance: $(pre-reqs)
	@gotestsum --format=testname -- -race -count=1 -timeout=3m -shuffle=on -tags=adversarial \
		-run '$(ACCEPTANCE_TESTS)' ./internal/ooze
.PHONY: test.acceptance

# Ten-repeat main, weekly, and manual gate for green, independently bounded
# process fixtures.
test.adversarial.stress: $(pre-reqs)
	@gotestsum --format=testname -- -race -count=$(STRESS_COUNT) -timeout=10m -shuffle=on \
		-run '$(ACCEPTANCE_TESTS)' \
		./internal/ooze
.PHONY: test.adversarial.stress

test.crosscompile: $(pre-reqs)
	@for target in linux/amd64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 plan9/amd64; do \
		GOOS="$${target%/*}" GOARCH="$${target#*/}" CGO_ENABLED=0 \
			go test -exec=true ./internal/ooze; \
	done
.PHONY: test.crosscompile

lint: $(pre-reqs)
	@golangci-lint -v run
.PHONY: lint
