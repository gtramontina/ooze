golangci-lint-version = v1.51.0

.bin/golangci-lint: makefile.golangci.mk
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
	| sh -s -- -b $(PWD)/$(dir $@) -d $(golangci-lint-version)
pre-reqs += .bin/golangci-lint
