golangci-lint-version = v1.52.2

.bin/golangci-lint: makefile.golangci.mk
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
	| sh -s -- -b $(PWD)/$(dir $@) -d $(golangci-lint-version)
pre-reqs += .bin/golangci-lint
